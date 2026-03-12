CREATE INDEX IF NOT EXISTS idx_video_actions_user_action_created
ON video_actions(user_id, action_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_follows_follower_created
ON follows(follower_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_video_progress_user_updated
ON user_video_progress(user_id, updated_at DESC);
