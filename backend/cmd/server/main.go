package main

import (
	"context"
	"log"
	"strconv"

	"arguehub/config"
	"arguehub/controllers"
	"arguehub/db"
	"arguehub/internal/debate"
	"arguehub/middlewares"
	"arguehub/routes"
	"arguehub/services"
	"arguehub/utils"
	"arguehub/websocket"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.LoadConfig("./config/config.prod.yml")
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}

	services.InitDebateVsBotService(cfg)
	services.InitCoachService()
	services.InitRatingService(cfg)

	if err := db.ConnectMongoDB(cfg.Database.URI); err != nil {
		panic("Failed to connect to MongoDB: " + err.Error())
	}
	log.Println("Connected to MongoDB")

	if err := db.EnsureIndexes(); err != nil {
		log.Fatalf("Failed to ensure indexes: %v", err)
	}

	if err := middlewares.InitCasbin("./config/config.prod.yml"); err != nil {
		log.Fatalf("Failed to initialize Casbin: %v", err)
	}
	log.Println("Casbin RBAC initialized")

	if cfg.Redis.Addr != "" {
		redisURL := cfg.Redis.Addr
		if redisURL == "" {
			redisURL = "localhost:6379"
		}
		if err := debate.InitRedis(redisURL, cfg.Redis.Password, cfg.Redis.DB); err != nil {
			log.Printf("⚠️ Warning: Failed to initialize Redis: %v", err)
			log.Printf("⚠️ Some realtime features will be unavailable until Redis is reachable")
		} else {
			log.Println("Connected to Redis")
		}
	} else {
		log.Println("Redis Addr not configured; continuing without Redis-backed features")
	}

	go websocket.WatchForNewRooms()

	utils.SetJWTSecret(cfg.JWT.Secret)
	utils.SeedDebateData()
	utils.PopulateTestUsers()

	var avatarStorage services.AvatarStorage
	if cfg.S3.HasEndpointConfig() && !cfg.S3.IsConfigured() {
		log.Fatal("Incomplete S3 avatar configuration: region and bucket are required")
	}
	if cfg.S3.IsConfigured() {
		storage, err := services.NewS3AvatarStorage(context.Background(), cfg.S3)
		if err != nil {
			log.Fatalf("Failed to initialize S3 avatar storage: %v", err)
		}
		avatarStorage = storage
		log.Println("S3 avatar storage initialized")
	} else {
		log.Println("S3 avatar storage is not configured; custom avatar uploads are disabled")
	}
	avatarController := controllers.NewAvatarController(avatarStorage)

	// Set up the Gin router and configure routes
	router := setupRouter(cfg, avatarController)
	port := strconv.Itoa(cfg.Server.Port)

	if err := router.Run(":" + port); err != nil {
		panic("Failed to start server: " + err.Error())
	}
}

func setupRouter(cfg *config.Config, avatarController *controllers.AvatarController) *gin.Engine {
	router := gin.Default()

	router.SetTrustedProxies([]string{"127.0.0.1", "localhost"})

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))
	router.OPTIONS("/*path", func(c *gin.Context) { c.Status(204) })

	router.POST("/signup", routes.SignUpRouteHandler)
	router.POST("/verifyEmail", routes.VerifyEmailRouteHandler)
	router.POST("/login", routes.LoginRouteHandler)
	router.POST("/googleLogin", routes.GoogleLoginRouteHandler)
	router.POST("/forgotPassword", routes.ForgotPasswordRouteHandler)
	router.POST("/confirmForgotPassword", routes.VerifyForgotPasswordRouteHandler)
	router.POST("/verifyToken", routes.VerifyTokenRouteHandler)

	router.GET("/debug/matchmaking-pool", routes.GetMatchmakingPoolStatusHandler)

	router.GET("/ws/matchmaking", websocket.MatchmakingHandler)
	router.GET("/ws/gamification", websocket.GamificationWebSocketHandler)

	auth := router.Group("/")
	auth.Use(middlewares.AuthMiddleware("./config/config.prod.yml"))
	{
		auth.GET("/user/fetchprofile", routes.GetProfileRouteHandler)
		auth.PUT("/user/updateprofile", routes.UpdateProfileRouteHandler)
		auth.GET("/user/check-displayname", routes.CheckDisplayNameRouteHandler)
		routes.SetupAvatarRoutes(auth, avatarController)
		auth.GET("/leaderboard", routes.GetLeaderboardRouteHandler)
		auth.POST("/debate/result", routes.UpdateRatingAfterDebateRouteHandler)

		auth.POST("/api/award-badge", routes.AwardBadgeRouteHandler)
		auth.POST("/api/update-score", routes.UpdateScoreRouteHandler)
		auth.GET("/api/leaderboard", routes.GetGamificationLeaderboardRouteHandler)

		routes.SetupDebateVsBotRoutes(auth)

		router.GET("/ws", websocket.WebsocketHandler)

		routes.SetupTranscriptRoutes(auth)

		auth.GET("/coach/strengthen-argument/weak-statement", routes.GetWeakStatement)
		auth.POST("/coach/strengthen-argument/evaluate", routes.EvaluateStrengthenedArgument)

		auth.GET("/rooms", routes.GetRoomsHandler)
		auth.POST("/rooms", routes.CreateRoomHandler)
		auth.POST("/rooms/:id/join", routes.JoinRoomHandler)
		auth.GET("/rooms/:id/participants", routes.GetRoomParticipantsHandler)

		routes.SetupTeamRoutes(auth)
		routes.SetupTeamDebateRoutes(auth)
		routes.SetupTeamChatRoutes(auth)
		routes.SetupTeamMatchmakingRoutes(auth)
		log.Println("Team routes registered")

		routes.SetupCommunityRoutes(auth)
		log.Println("Community routes registered")

		auth.GET("/notifications", routes.GetNotificationsRouteHandler)
		auth.PUT("/notifications/:id/read", routes.MarkNotificationAsReadRouteHandler)
		auth.PUT("/notifications/read-all", routes.MarkAllNotificationsAsReadRouteHandler)
		auth.DELETE("/notifications/:id", routes.DeleteNotificationRouteHandler)
	}

	router.GET("/ws/team", websocket.TeamWebsocketHandler)

	routes.SetupAdminRoutes(router, "./config/config.prod.yml")
	log.Println("Admin routes registered")

	router.GET("/ws/debate/:debateID", websocket.DebateWebsocketHandler)

	return router
}
