package main

import (
	"encoding/json"
	"fmt"
	"googlecalenderservice/pkg/config"
	"googlecalenderservice/pkg/models"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	googleOauthConfig *oauth2.Config
	store             *session.Store
	db                *gorm.DB
)

func init() {
	cfg, configerr := config.LoadConfig()
	if configerr != nil {
		log.Fatalf("Failed to load config: %v", configerr)
	}
	// OAuth2 configuration
	googleOauthConfig = &oauth2.Config{
		RedirectURL:  cfg.RedirectURL,
		ClientID:     cfg.GoogleClientId,
		ClientSecret: cfg.GoogleSecretId,
		Scopes: []string{
			calendar.CalendarScope,
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	// Initialize session store
	store = session.New()

	// Initialize GORM with PostgreSQL
	var err error
	dsn := fmt.Sprintf("host=%s user=%s dbname=%s port=%s password=%s", cfg.DBHost, cfg.DBUser, cfg.DBName, cfg.DBPort, cfg.DBPassword)
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}

	// AutoMigrate the User model
	db.AutoMigrate(&models.User{}, &models.Event{})
}

func handleGoogleLogin(c *fiber.Ctx) error {
	url := googleOauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	return c.Redirect(url)
}

func handleGoogleCallback(c *fiber.Ctx) error {
	code := c.Query("code")
	if code == "" {
		log.Printf("No code in query parameters")
		return c.Status(fiber.StatusBadRequest).SendString("No code in query parameters")
	}

	token, err := googleOauthConfig.Exchange(c.Context(), code)
	if err != nil {
		log.Printf("Failed to exchange token: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to exchange token")
	}

	// Retrieve user info using the token
	client := googleOauthConfig.Client(c.Context(), token)
	userInfoResponse, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		log.Printf("Unable to retrieve user info: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Unable to retrieve user info")
	}
	defer userInfoResponse.Body.Close()

	var userInfo struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		GivenName     string `json:"given_name"`
		FamilyName    string `json:"family_name"`
		Link          string `json:"link"`
		Picture       string `json:"picture"`
		Locale        string `json:"locale"`
		HD            string `json:"hd"`
	}

	if err := json.NewDecoder(userInfoResponse.Body).Decode(&userInfo); err != nil {
		log.Printf("Unable to parse user info: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Unable to parse user info")
	}

	googleID := userInfo.ID
	googleEmail := userInfo.Email

	// Store the tokens in the database
	user := models.User{
		GoogleID:     googleID,
		GoogleEmail:  googleEmail,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenExpiry:  token.Expiry,
	}
	if err := db.Where(models.User{GoogleID: googleID}).Assign(user).FirstOrCreate(&user).Error; err != nil {
		log.Printf("Failed to store token in the database: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to store token in the database")
	}

	// Store the user ID in session
	sess, err := store.Get(c)
	if err != nil {
		log.Printf("Failed to get session: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to get session")
	}
	sess.Set("user_id", googleID)
	if err := sess.Save(); err != nil {
		log.Printf("Failed to save session: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to save session")
	}

	// Redirect the user to Google Calendar interface
	return c.Redirect("https://calendar.google.com")
}
func handleCreateEvent(c *fiber.Ctx) error {

	userID := c.Get("X-User-Id")
	if userID == "" {
		log.Printf("user_id not found in header")
		return c.Status(fiber.StatusUnauthorized).SendString("user_id not found in header")
	}

	// Get the event details from the request
	eventSummary := c.FormValue("summary")
	eventDescription := c.FormValue("description")
	startTime, _ := time.Parse(time.RFC3339, c.FormValue("start"))
	endTime, _ := time.Parse(time.RFC3339, c.FormValue("end"))
	guestEmail := c.FormValue("guest")
	// guestEmails := strings.Split(c.FormValue("guests"), ",")

	// Retrieve the user's access token from the database
	var user models.User
	if err := db.Where("google_id = ?", userID).First(&user).Error; err != nil {
		log.Printf("Failed to retrieve user: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to retrieve user")
	}

	// Create a new Google Calendar API client
	config := &oauth2.Config{
		ClientID:     googleOauthConfig.ClientID,
		ClientSecret: googleOauthConfig.ClientSecret,
		RedirectURL:  googleOauthConfig.RedirectURL,
		Scopes:       googleOauthConfig.Scopes,
		Endpoint:     google.Endpoint,
	}
	token := &oauth2.Token{
		AccessToken:  user.AccessToken,
		RefreshToken: user.RefreshToken,
		Expiry:       user.TokenExpiry,
	}
	client := config.Client(c.Context(), token)
	calendarService, err := calendar.New(client)
	if err != nil {
		log.Printf("Unable to create calendar service: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Unable to create calendar service")
	}

	// Create a new event using the Google Calendar API
	event := &calendar.Event{
		Summary:     eventSummary,
		Description: eventDescription,
		Start: &calendar.EventDateTime{
			DateTime: startTime.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: endTime.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Attendees: []*calendar.EventAttendee{
            {Email: guestEmail}, // Change from a slice to a single attendee
        },
		// Attendees: make([]*calendar.EventAttendee, len(guestEmails)),
	}
	// for i, email := range guestEmails {
	// 	event.Attendees[i] = &calendar.EventAttendee{Email: email}
	// }
	createdEvent, err := calendarService.Events.Insert("primary", event).Do()
	if err != nil {
		log.Printf("Unable to create event: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("Unable to create event")
	}

	// Store the event details in the database
	newEvent := models.Event{
        UserID:      user.ID,
        EventID:     createdEvent.Id,
        Summary:     eventSummary,
        Description: eventDescription,
        Start:       startTime,
        End:         endTime,
		GuestEmail:  guestEmail,
        CreatedAt:   time.Now(),
    }
	if err := db.Create(&newEvent).Error; err != nil {
        log.Printf("Failed to store event in the database: %v", err)
        return c.Status(fiber.StatusInternalServerError).SendString("Failed to store event in the database")
    }

	//  // Insert the event details without the GuestEmails field
	//  if err := db.Create(&newEvent).Omit("GuestEmails").Error; err != nil {
    //     log.Printf("Failed to store event in the database: %v", err)
    //     return c.Status(fiber.StatusInternalServerError).SendString("Failed to store event in the database")
    // }

    // // Convert the GuestEmails slice to a PostgreSQL array literal
    // guestEmailsArray := pq.Array(guestEmails)

    // // Update the GuestEmails field separately
    // if err := db.Model(&newEvent).UpdateColumn("guest_emails", guestEmailsArray).Error; err != nil {
    //     log.Printf("Failed to store guest_emails in the database: %v", err)
    //     return c.Status(fiber.StatusInternalServerError).SendString("Failed to store guest_emails in the database")
    // }

    return c.Status(fiber.StatusOK).JSON(createdEvent)

}
func main() {
	app := fiber.New()

	// Routes
	app.Get("/google-login", handleGoogleLogin)
	app.Get("/google/redirect", handleGoogleCallback)
	app.Post("/create-event", handleCreateEvent)

	// Start server
	log.Fatal(app.Listen(":8000"))
}
