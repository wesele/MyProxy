package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/models"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		keyValue := extractBearerToken(c)
		if keyValue == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
			return
		}

		apiKey, err := db.VerifyApiKey(keyValue)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			return
		}

		c.Set("api_key", apiKey)
		c.Next()
	}
}

func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if keyValue := extractBearerToken(c); keyValue != "" {
			apiKey, err := db.VerifyApiKey(keyValue)
			if err == nil {
				c.Set("api_key", apiKey)
				c.Next()
				return
			}
		}

		ip := c.ClientIP()
		if ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "10.") {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required for remote access"})
	}
}

func extractBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return ""
}

func GetApiKey(c *gin.Context) *models.ApiKey {
	key, _ := c.Get("api_key")
	if key == nil {
		return nil
	}
	return key.(*models.ApiKey)
}
