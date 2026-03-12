package models

type TokenPair struct {
	AccessToken      string `json:"access_token"`
	AccessExpiresAt  string `json:"access_expires_at"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresAt string `json:"refresh_expires_at"`
}

type UserBrief struct {
	ID             string `json:"id"`
	Username       string `json:"username"`
	Email          string `json:"email,omitempty"`
	Role           string `json:"role,omitempty"`
	Bio            string `json:"bio"`
	AvatarURL      string `json:"avatar_url,omitempty"`
	FollowersCount int64  `json:"followers_count"`
	FollowingCount int64  `json:"following_count"`
}

type UploadTicket struct {
	UploadID     string            `json:"upload_id"`
	Provider     string            `json:"provider"`
	Method       string            `json:"method"`
	UploadURL    string            `json:"upload_url"`
	Headers      map[string]string `json:"headers"`
	ObjectKey    string            `json:"object_key"`
	ExpiresAt    string            `json:"expires_at"`
	MaxSizeBytes int64             `json:"max_size_bytes"`
}

type Author struct {
	ID             string `json:"id"`
	Username       string `json:"username"`
	AvatarURL      string `json:"avatar_url,omitempty"`
	FollowersCount int64  `json:"followers_count"`
	Followed       bool   `json:"followed"`
}

type VideoCard struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	CoverURL       string `json:"cover_url,omitempty"`
	PreviewWebPURL string `json:"preview_webp_url,omitempty"`
	DurationSec    int64  `json:"duration_sec"`
	Author         Author `json:"author"`
	ViewsCount     int64  `json:"views_count"`
	CommentsCount  int64  `json:"comments_count"`
	PublishedAt    string `json:"published_at"`
	Category       string `json:"category"`
}

type VideoStats struct {
	ViewsCount     int64 `json:"views_count"`
	LikesCount     int64 `json:"likes_count"`
	FavoritesCount int64 `json:"favorites_count"`
	CommentsCount  int64 `json:"comments_count"`
	SharesCount    int64 `json:"shares_count"`
}

type ViewerActions struct {
	Liked             bool `json:"liked"`
	Favorited         bool `json:"favorited"`
	FollowingUploader bool `json:"following_uploader"`
}

type VideoDetail struct {
	Video             VideoCard     `json:"video"`
	SourceURL         string        `json:"source_url"`
	ViewerProgressSec int64         `json:"viewer_progress_sec,omitempty"`
	Description       string        `json:"description"`
	Tags              []string      `json:"tags"`
	Stats             VideoStats    `json:"stats"`
	Uploader          Author        `json:"uploader"`
	ViewerActions     ViewerActions `json:"viewer_actions"`
}

type CommentItem struct {
	ID              string        `json:"id"`
	VideoID         string        `json:"video_id"`
	User            UserBrief     `json:"user"`
	Content         string        `json:"content"`
	LikeCount       int64         `json:"like_count"`
	CreatedAt       string        `json:"created_at"`
	ParentCommentID *string       `json:"parent_comment_id,omitempty"`
	Replies         []CommentItem `json:"replies,omitempty"`
}
