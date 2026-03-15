package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/middleware"
)

type Handler struct {
	app        *app.App
	danmakuHub *danmakuHub
}

func New(a *app.App) *Handler {
	return &Handler{
		app:        a,
		danmakuHub: newDanmakuHub(),
	}
}

func RegisterRoutes(api fiber.Router, a *app.App) {
	h := New(a)

	api.Get("/auth/captcha", h.rateLimit(rlAuthCaptchaIP), h.GetAuthCaptcha)
	api.Post("/auth/register", h.rateLimit(rlAuthRegIP), h.Register)
	api.Post("/auth/login", h.rateLimit(rlAuthLoginIP), h.rateLimit(rlAuthLoginAcct), h.Login)
	api.Post("/auth/refresh", h.rateLimit(rlAuthRefreshIP), h.Refresh)
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
	api.Get("/tags", h.ListTags)
	api.Get("/site-settings/public", h.GetPublicSiteSettings)
	api.Get("/rankings/hot", h.GetHotRankings)

	api.Get("/videos", middleware.OptionalAuth(a), h.ListVideos)
	api.Get("/videos/:videoId", middleware.OptionalAuth(a), h.GetVideoDetail)
	api.Get("/videos/:videoId/recommendations", middleware.OptionalAuth(a), h.GetVideoRecommendations)
	api.Post("/videos/:videoId/view", h.rateLimit(rlViewRate), h.TrackVideoView)
	api.Put("/videos/:videoId/like", middleware.RequireAuth(a), h.rateLimit(rlInteractionUser), h.ToggleVideoLike)
	api.Put("/videos/:videoId/favorite", middleware.RequireAuth(a), h.rateLimit(rlInteractionUser), h.ToggleVideoFavorite)
	api.Post("/videos/:videoId/share", middleware.OptionalAuth(a), h.rateLimit(rlShareRate), h.TrackVideoShare)
	api.Put("/videos/:videoId/progress", middleware.RequireAuth(a), h.rateLimit(rlProgressRate), h.UpdateVideoProgress)
	api.Post("/videos", middleware.RequireAuth(a), h.CreateVideo)
	api.Patch("/videos/:videoId", middleware.RequireAuth(a), h.UpdateVideo)
	api.Delete("/videos/:videoId", middleware.RequireAuth(a), h.DeleteVideo)

	api.Post("/imports/torrent/inspect", middleware.RequireAuth(a), h.rateLimit(rlImportInspectUser), h.rateLimit(rlImportInspectIP), h.InspectTorrentImport)
	api.Post("/imports/torrent/start", middleware.RequireAuth(a), h.rateLimit(rlImportStartT), h.StartTorrentImport)
	api.Post("/imports/url/start", middleware.RequireAuth(a), h.rateLimit(rlImportStartURL), h.StartURLImport)
	api.Get("/imports", middleware.RequireAuth(a), h.ListImportJobs)
	api.Delete("/imports", middleware.RequireAuth(a), h.ClearFinishedImportJobs)
	api.Get("/imports/:jobId", middleware.RequireAuth(a), h.GetImportJobDetail)

	api.Get("/videos/:videoId/comments", middleware.OptionalAuth(a), h.ListComments)
	api.Post("/videos/:videoId/comments", middleware.RequireAuth(a), h.rateLimit(rlCommentCreate), h.rateLimit(rlCommentBurst), h.CreateComment)
	api.Get("/videos/:videoId/danmaku", middleware.OptionalAuth(a), h.ListVideoDanmaku)
	api.Get("/videos/:videoId/danmaku/list", middleware.OptionalAuth(a), h.ListVideoDanmakuTimeline)
	api.Post("/videos/:videoId/danmaku", middleware.RequireAuth(a), h.rateLimit(rlDanmakuCreate), h.rateLimit(rlDanmakuBurst), h.CreateVideoDanmaku)
	api.Use("/videos/:videoId/danmaku/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	api.Get("/videos/:videoId/danmaku/ws", websocket.New(h.SubscribeVideoDanmakuWS))
	api.Put("/comments/:commentId/like", middleware.RequireAuth(a), h.rateLimit(rlInteractionUser), h.ToggleCommentLike)
	api.Delete("/comments/:commentId", middleware.RequireAuth(a), h.DeleteComment)

	api.Post("/uploads/presign", middleware.RequireAuth(a), h.CreateUploadPresign)
	api.Put("/uploads/local/:uploadToken", h.UploadToLocal)
	api.Post("/uploads/:uploadId/complete", middleware.RequireAuth(a), h.CompleteUpload)

	admin := api.Group("/admin", middleware.RequireAuth(a), middleware.RequireAdmin(a))
	h.RegisterAdminRoutes(admin)
}
