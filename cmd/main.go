package main

import (
	"encoding/json"
	"fmt"
	"googlecalenderservice/pkg/config"
	"googlecalenderservice/pkg/models"
	"log"

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
	db.AutoMigrate(&models.User{})
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

func main() {
	app := fiber.New()

	// Routes
	app.Get("/google-login", handleGoogleLogin)
	app.Get("/google/redirect", handleGoogleCallback)

	// Start server
	log.Fatal(app.Listen(":8000"))
}
