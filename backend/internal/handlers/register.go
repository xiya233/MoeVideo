package handlers

import (
	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/middleware"
)

type Handler struct {
	app *app.App
}

func New(a *app.App) *Handler {
	return &Handler{app: a}
}

func RegisterRoutes(api fiber.Router, a *app.App) {
	h := New(a)

	api.Get("/auth/captcha", h.GetAuthCaptcha)
	api.Post("/auth/register", h.Register)
	api.Post("/auth/login", h.Login)
	api.Post("/auth/refresh", h.Refresh)
	api.Post("/auth/logout", middleware.RequireAuth(a), h.Logout)

	api.Get("/users/me", middleware.RequireAuth(a), h.GetMe)
	api.Patch("/users/me", middleware.RequireAuth(a), h.UpdateMe)
	api.Get("/users/me/videos", middleware.RequireAuth(a), h.ListMyVideos)
	api.Get("/users/me/favorites", middleware.RequireAuth(a), h.ListMyFavorites)
	api.Get("/users/me/following", middleware.RequireAuth(a), h.ListMyFollowing)
	api.Get("/users/me/followers", middleware.RequireAuth(a), h.ListMyFollowers)
	api.Get("/users/me/continue-watching", middleware.RequireAuth(a), h.ListMyContinueWatching)
	api.Get("/users/:userId", middleware.OptionalAuth(a), h.GetUserByID)
	api.Get("/users/:userId/videos", middleware.OptionalAuth(a), h.ListUserVideos)
	api.Get("/users/:userId/favorites", middleware.OptionalAuth(a), h.ListUserFavorites)
	api.Get("/users/:userId/following", middleware.OptionalAuth(a), h.ListUserFollowing)
	api.Get("/users/:userId/followers", middleware.OptionalAuth(a), h.ListUserFollowers)
	api.Put("/users/:userId/follow", middleware.RequireAuth(a), h.ToggleFollow)

	api.Get("/home", middleware.OptionalAuth(a), h.GetHome)
	api.Get("/categories", h.ListCategories)
	api.Get("/site-settings/public", h.GetPublicSiteSettings)
	api.Get("/rankings/hot", h.GetHotRankings)

	api.Get("/videos", middleware.OptionalAuth(a), h.ListVideos)
	api.Get("/videos/:videoId", middleware.OptionalAuth(a), h.GetVideoDetail)
	api.Get("/videos/:videoId/recommendations", h.GetVideoRecommendations)
	api.Post("/videos/:videoId/view", h.TrackVideoView)
	api.Put("/videos/:videoId/like", middleware.RequireAuth(a), h.ToggleVideoLike)
	api.Put("/videos/:videoId/favorite", middleware.RequireAuth(a), h.ToggleVideoFavorite)
	api.Post("/videos/:videoId/share", h.TrackVideoShare)
	api.Put("/videos/:videoId/progress", middleware.RequireAuth(a), h.UpdateVideoProgress)
	api.Post("/videos", middleware.RequireAuth(a), h.CreateVideo)
	api.Delete("/videos/:videoId", middleware.RequireAuth(a), h.DeleteVideo)

	api.Get("/videos/:videoId/comments", middleware.OptionalAuth(a), h.ListComments)
	api.Post("/videos/:videoId/comments", middleware.RequireAuth(a), h.CreateComment)
	api.Put("/comments/:commentId/like", middleware.RequireAuth(a), h.ToggleCommentLike)
	api.Delete("/comments/:commentId", middleware.RequireAuth(a), h.DeleteComment)

	api.Post("/uploads/presign", middleware.RequireAuth(a), h.CreateUploadPresign)
	api.Put("/uploads/local/:uploadToken", h.UploadToLocal)
	api.Post("/uploads/:uploadId/complete", middleware.RequireAuth(a), h.CompleteUpload)

	admin := api.Group("/admin", middleware.RequireAuth(a), middleware.RequireAdmin(a))
	h.RegisterAdminRoutes(admin)
}
