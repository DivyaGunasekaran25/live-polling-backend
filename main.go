package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Email        string        `bson:"email" json:"email"`
	PasswordHash string        `bson:"passwordHash" json:"-"`
	Token        string        `bson:"token" json:"-"`
}

type Poll struct {
	ID       bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Question string        `bson:"question" json:"question"`
	Options  []string      `bson:"options" json:"options"`
	Votes    []int         `bson:"votes" json:"votes"`
	OwnerID  bson.ObjectID `bson:"ownerId,omitempty" json:"ownerId,omitempty"`
}

var collection *mongo.Collection
var usersCollection *mongo.Collection
var redisClient *redis.Client

func generateToken() (string, error) {
	bytes := make([]byte, 32)

	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "Login required",
			})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)

		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "Invalid authorization header",
			})
			c.Abort()
			return
		}

		token := parts[1]

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()

		var user User

		err := usersCollection.FindOne(
			ctx,
			bson.M{"token": token},
		).Decode(&user)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "Invalid or expired login",
			})
			c.Abort()
			return
		}

		c.Set("userID", user.ID)

		c.Next()
	}
}

func main() {

	// =========================================================
	// MONGODB
	// =========================================================

	mongoURI := os.Getenv("MONGODB_URI")

	// Keep local development working.
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
		fmt.Println("MONGODB_URI not set. Using local MongoDB.")
	} else {
		fmt.Println("Using MongoDB URI from environment.")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	client, err := mongo.Connect(
		options.Client().ApplyURI(mongoURI),
	)

	if err != nil {
		panic(err)
	}

	err = client.Ping(ctx, nil)

	if err != nil {
		panic(fmt.Sprintf(
			"MongoDB connection failed: %v",
			err,
		))
	}

	fmt.Println("MongoDB connected successfully!")

	database := client.Database("livepolling")

	collection = database.Collection("polls")
	usersCollection = database.Collection("users")

	// =========================================================
	// REDIS
	// =========================================================

	redisURL := os.Getenv("REDIS_URL")

	if redisURL == "" {
		fmt.Println("REDIS_URL not set. Using local Redis.")

		redisClient = redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		})
	} else {
		fmt.Println("Using Redis URL from environment.")

		redisOptions, err := redis.ParseURL(redisURL)

		if err != nil {
			panic(fmt.Sprintf(
				"Invalid REDIS_URL: %v",
				err,
			))
		}

		redisClient = redis.NewClient(redisOptions)
	}

	redisCtx, redisCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer redisCancel()

	err = redisClient.Ping(redisCtx).Err()

	if err != nil {
		panic(fmt.Sprintf(
			"Redis connection failed: %v",
			err,
		))
	}

	fmt.Println("Redis connected successfully!")

	// =========================================================
	// GIN
	// =========================================================

	r := gin.Default()

	// =========================================================
	// CORS
	// =========================================================

	frontendURL := os.Getenv("FRONTEND_URL")

	r.Use(func(c *gin.Context) {

		origin := c.GetHeader("Origin")

		allowed := false

		// Local development
		if origin == "http://localhost:5173" ||
			origin == "http://localhost:5174" ||
			origin == "http://localhost:5175" {
			allowed = true
		}

		// Deployed frontend
		if frontendURL != "" && origin == frontendURL {
			allowed = true
		}

		if allowed {
			c.Writer.Header().Set(
				"Access-Control-Allow-Origin",
				origin,
			)
		}

		c.Writer.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, POST, OPTIONS",
		)

		c.Writer.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)

		c.Writer.Header().Set(
			"Access-Control-Allow-Credentials",
			"true",
		)

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// =========================================================
	// HOME
	// =========================================================

	r.GET("/", func(c *gin.Context) {

		c.JSON(http.StatusOK, gin.H{
			"message": "Live Polling Backend is working!",
		})

	})

	// =========================================================
	// SIGNUP
	// =========================================================

	r.POST("/auth/signup", func(c *gin.Context) {

		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Invalid signup data",
			})

			return
		}

		request.Email = strings.ToLower(
			strings.TrimSpace(request.Email),
		)

		if request.Email == "" || request.Password == "" {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Email and password are required",
			})

			return
		}

		if len(request.Password) < 6 {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Password must be at least 6 characters",
			})

			return
		}

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)

		defer cancel()

		var existingUser User

		err := usersCollection.FindOne(
			ctx,
			bson.M{"email": request.Email},
		).Decode(&existingUser)

		if err == nil {

			c.JSON(http.StatusConflict, gin.H{
				"message": "Email already registered",
			})

			return
		}

		passwordHash, err := bcrypt.GenerateFromPassword(
			[]byte(request.Password),
			bcrypt.DefaultCost,
		)

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Could not create account",
			})

			return
		}

		token, err := generateToken()

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Could not create login token",
			})

			return
		}

		user := User{
			ID:           bson.NewObjectID(),
			Email:        request.Email,
			PasswordHash: string(passwordHash),
			Token:        token,
		}

		_, err = usersCollection.InsertOne(
			ctx,
			user,
		)

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Failed to create account",
			})

			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"message": "Account created successfully!",
			"token":   token,
			"email":   user.Email,
		})

	})

	// =========================================================
	// LOGIN
	// =========================================================

	r.POST("/auth/login", func(c *gin.Context) {

		var request struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Invalid login data",
			})

			return
		}

		request.Email = strings.ToLower(
			strings.TrimSpace(request.Email),
		)

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)

		defer cancel()

		var user User

		err := usersCollection.FindOne(
			ctx,
			bson.M{"email": request.Email},
		).Decode(&user)

		if err != nil {

			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "Invalid email or password",
			})

			return
		}

		err = bcrypt.CompareHashAndPassword(
			[]byte(user.PasswordHash),
			[]byte(request.Password),
		)

		if err != nil {

			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "Invalid email or password",
			})

			return
		}

		token, err := generateToken()

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Could not create login token",
			})

			return
		}

		_, err = usersCollection.UpdateOne(
			ctx,
			bson.M{"_id": user.ID},
			bson.M{
				"$set": bson.M{
					"token": token,
				},
			},
		)

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Could not update login",
			})

			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Login successful!",
			"token":   token,
			"email":   user.Email,
		})
	})

	// =========================================================
	// CREATE POLL
	// =========================================================

	r.POST(
		"/polls",
		authMiddleware(),
		func(c *gin.Context) {

			var poll Poll

			if err := c.ShouldBindJSON(&poll); err != nil {

				c.JSON(http.StatusBadRequest, gin.H{
					"message": "Invalid poll data",
					"error":   err.Error(),
				})

				return
			}

			if strings.TrimSpace(poll.Question) == "" ||
				len(poll.Options) < 2 {

				c.JSON(http.StatusBadRequest, gin.H{
					"message": "Question and at least 2 options are required",
				})

				return
			}

			for i := range poll.Options {

				poll.Options[i] = strings.TrimSpace(
					poll.Options[i],
				)

				if poll.Options[i] == "" {

					c.JSON(http.StatusBadRequest, gin.H{
						"message": "Options cannot be empty",
					})

					return
				}
			}

			poll.ID = bson.NewObjectID()
			poll.Votes = make([]int, len(poll.Options))

			if userID, exists := c.Get("userID"); exists {

				poll.OwnerID = userID.(bson.ObjectID)

			}

			ctx, cancel := context.WithTimeout(
				context.Background(),
				10*time.Second,
			)

			defer cancel()

			_, err := collection.InsertOne(
				ctx,
				poll,
			)

			if err != nil {

				c.JSON(http.StatusInternalServerError, gin.H{
					"message": "Failed to create poll",
					"error":   err.Error(),
				})

				return
			}

			c.JSON(http.StatusCreated, gin.H{
				"message": "Poll created successfully!",
				"poll":    poll,
			})

		},
	)

	// =========================================================
	// GET ALL POLLS
	// =========================================================

	r.GET("/polls", func(c *gin.Context) {

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)

		defer cancel()

		cursor, err := collection.Find(
			ctx,
			bson.M{},
		)

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Failed to get polls",
				"error":   err.Error(),
			})

			return
		}

		defer cursor.Close(ctx)

		var polls []Poll

		if err := cursor.All(ctx, &polls); err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Failed to read polls",
				"error":   err.Error(),
			})

			return
		}

		c.JSON(http.StatusOK, polls)

	})

	// =========================================================
	// GET ONE POLL
	// =========================================================

	r.GET("/polls/:id", func(c *gin.Context) {

		id, err := bson.ObjectIDFromHex(
			c.Param("id"),
		)

		if err != nil {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Invalid poll ID",
			})

			return
		}

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)

		defer cancel()

		var poll Poll

		err = collection.FindOne(
			ctx,
			bson.M{"_id": id},
		).Decode(&poll)

		if err != nil {

			c.JSON(http.StatusNotFound, gin.H{
				"message": "Poll not found",
			})

			return
		}

		c.JSON(http.StatusOK, poll)

	})

	// =========================================================
	// VOTE
	// =========================================================

	r.POST("/polls/:id/vote", func(c *gin.Context) {

		id, err := bson.ObjectIDFromHex(
			c.Param("id"),
		)

		if err != nil {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Invalid poll ID",
			})

			return
		}

		var request struct {
			OptionIndex int `json:"optionIndex"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Invalid vote data",
			})

			return
		}

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)

		defer cancel()

		var poll Poll

		err = collection.FindOne(
			ctx,
			bson.M{"_id": id},
		).Decode(&poll)

		if err != nil {

			c.JSON(http.StatusNotFound, gin.H{
				"message": "Poll not found",
			})

			return
		}

		if request.OptionIndex < 0 ||
			request.OptionIndex >= len(poll.Options) {

			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Invalid option",
			})

			return
		}

		if len(poll.Votes) != len(poll.Options) {

			poll.Votes = make(
				[]int,
				len(poll.Options),
			)

		}

		poll.Votes[request.OptionIndex]++

		_, err = collection.UpdateOne(
			ctx,
			bson.M{"_id": id},
			bson.M{
				"$set": bson.M{
					"votes": poll.Votes,
				},
			},
		)

		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "Failed to save vote",
				"error":   err.Error(),
			})

			return
		}

		// =====================================================
		// PUBLISH LIVE UPDATE THROUGH REDIS
		// =====================================================

		eventData := gin.H{
			"pollId": id.Hex(),
			"votes":  poll.Votes,
		}

		eventJSON, err := json.Marshal(eventData)

		if err == nil {

			channel := "poll:" + id.Hex()

			err = redisClient.Publish(
				ctx,
				channel,
				eventJSON,
			).Err()

			if err != nil {

				fmt.Println(
					"Redis publish error:",
					err,
				)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Vote recorded successfully!",
			"votes":   poll.Votes,
		})

	})

	// =========================================================
	// LIVE REDIS EVENTS / SERVER-SENT EVENTS
	// =========================================================

	r.GET("/polls/:id/events", func(c *gin.Context) {

		id := c.Param("id")

		channel := "poll:" + id

		pubsub := redisClient.Subscribe(
			c.Request.Context(),
			channel,
		)

		defer pubsub.Close()

		fmt.Println(
			"Client connected to:",
			channel,
		)

		c.Writer.Header().Set(
			"Content-Type",
			"text/event-stream",
		)

		c.Writer.Header().Set(
			"Cache-Control",
			"no-cache",
		)

		c.Writer.Header().Set(
			"Connection",
			"keep-alive",
		)

		c.Writer.Header().Set(
			"X-Accel-Buffering",
			"no",
		)

		c.Writer.WriteString(
			"event: connected\n\n",
		)

		c.Writer.Flush()

		for {

			msg, err := pubsub.ReceiveMessage(
				c.Request.Context(),
			)

			if err != nil {

				fmt.Println(
					"Redis subscription closed:",
					err,
				)

				return
			}

			c.Writer.WriteString(
				"data: " + msg.Payload + "\n\n",
			)

			c.Writer.Flush()
		}
	})

	// =========================================================
	// SERVER PORT
	// =========================================================

	port := os.Getenv("PORT")

	// Render provides PORT automatically.
	// Local development uses 8081.
	if port == "" {
		port = "8081"
	}

	fmt.Println(
		"Starting Live Polling backend on port " + port + "...",
	)

	if err := r.Run(":" + port); err != nil {

		fmt.Println(
			"Server error:",
			err,
		)

	}
}
