package models

import (
    "time"
    "gorm.io/gorm"
)

type User struct {
    gorm.Model
    GoogleID     string `gorm:"unique;not null"`
    GoogleEmail string `gorm:"unique;not null"`
    AccessToken  string `gorm:"not null"`
    RefreshToken string `gorm:"not null"`
    TokenExpiry  time.Time `gorm:"not null"`
}
type Event struct {
    gorm.Model
    UserID         uint
    EventID        string `gorm:"unique;not null"`
    Summary        string
    Description    string
    Start          time.Time
    End            time.Time
    GuestEmails    []string `gorm:"type:text[]"`
    CreatedAt      time.Time
}