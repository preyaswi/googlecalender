package config

import "github.com/spf13/viper"

type Config struct {
	DBHost         string `mapstructure:"DB_HOST"`
	DBName         string `mapstructure:"DB_NAME"`
	DBUser         string `mapstructure:"DB_USER"`
	DBPort         string `mapstructure:"DB_PORT"`
	DBPassword     string `mapstructure:"DB_PASSWORD"`
	GoogleClientId string `mapstructure:"YOUR_CLIENT_ID"`
	GoogleSecretId string `mapstructure:"YOUR_CLIENT_SECRET"`
	RedirectURL    string `mapstructure:"REDIRECT_URL"`
}

var envs = []string{
	"DB_HOST", "DB_NAME", "DB_USER", "DB_PORT", "DB_PASSWORD", "YOUR_CLIENT_ID", "YOUR_CLIENT_SECRET", "REDIRECT_URL",
}

func LoadConfig() (Config, error) {
	var config Config
	viper.AddConfigPath("./")
	viper.SetConfigFile(".env")
	viper.ReadInConfig()
	for _, env := range envs {
		if err := viper.BindEnv(env); err != nil {
			return config, err
		}
	}
	if err := viper.Unmarshal(&config); err != nil {
		return config, err
	}
	return config, nil
}
