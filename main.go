package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	_ "github.com/jackc/pgx/v4/stdlib"
)

var (
	db        *sql.DB
	rdb       *redis.Client
	letters   = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	dbContext = context.Background()
)

func init() {
	rand.Seed(time.Now().UnixNano())

	// Initialize Redis client
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Check Redis connection
	if err := rdb.Ping(dbContext).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Initialize PostgreSQL database
	connStr := "host=localhost port=5432 user=postgres password=magic dbname=urlshortener sslmode=disable"
	var err error
	db, err = sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Check PostgreSQL connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	// Create url_mappings table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS url_mappings (
		id SERIAL PRIMARY KEY,
		short_url VARCHAR(255) UNIQUE NOT NULL,
		original_url VARCHAR(255) NOT NULL
	);`)
	if err != nil {
		log.Fatalf("Failed to create url_mappings table: %v", err)
	}
}

func main() {
	r := gin.Default()

	r.POST("/shorten", shortenURL)
	r.GET("/:shortURL", redirectURL)

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func shortenURL(c *gin.Context) {
	var req struct {
		URL string `json:"url" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		log.Printf("Invalid request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	shortURL, err := generateShortURL(req.URL)
	if err != nil {
		log.Printf("Failed to generate short URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate short URL"})
		return
	}

	err = storeURLMapping(shortURL, req.URL)
	if err != nil {
		log.Printf("Failed to store URL mapping: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store URL mapping"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"shortURL": shortURL})
}

func generateShortURL(url string) (string, error) {
	key := generateRandomKey(6)
	err := rdb.Set(dbContext, key, url, 0).Err()
	if err == redis.Nil {
		return key, nil
	} else if err != nil {
		log.Printf("Failed to set value in Redis: %v", err)
		return "", fmt.Errorf("failed to set value in Redis: %w", err)
	}
	return key, nil
}

func generateRandomKey(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func storeURLMapping(shortURL, originalURL string) error {
	_, err := db.Exec("INSERT INTO url_mappings (short_url, original_url) VALUES ($1, $2)", shortURL, originalURL)
	if err != nil {
		log.Printf("Failed to insert URL mapping into database: %v", err)
		return fmt.Errorf("failed to insert URL mapping into database: %w", err)
	}
	return nil
}

func redirectURL(c *gin.Context) {
	shortURL := c.Param("shortURL")

	originalURL, err := getOriginalURL(shortURL)
	if err != nil {
		log.Printf("Failed to get original URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get original URL"})
		return
	}

	if originalURL == "" {
		log.Printf("URL not found for short URL: %s", shortURL)
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	}

	c.Redirect(http.StatusMovedPermanently, originalURL)
}

func getOriginalURL(shortURL string) (string, error) {
	var originalURL string
	err := rdb.Get(dbContext, shortURL).Scan(&originalURL)
	if err == redis.Nil {
		return "", nil
	} else if err != nil {
		log.Printf("Failed to get value from Redis: %v", err)
		return "", err
	}
	return originalURL, nil
}
