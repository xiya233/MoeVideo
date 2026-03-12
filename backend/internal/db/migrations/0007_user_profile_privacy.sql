ALTER TABLE users ADD COLUMN profile_public INTEGER NOT NULL DEFAULT 1 CHECK (profile_public IN (0, 1));
ALTER TABLE users ADD COLUMN public_videos INTEGER NOT NULL DEFAULT 1 CHECK (public_videos IN (0, 1));
ALTER TABLE users ADD COLUMN public_favorites INTEGER NOT NULL DEFAULT 0 CHECK (public_favorites IN (0, 1));
ALTER TABLE users ADD COLUMN public_following INTEGER NOT NULL DEFAULT 0 CHECK (public_following IN (0, 1));
ALTER TABLE users ADD COLUMN public_followers INTEGER NOT NULL DEFAULT 0 CHECK (public_followers IN (0, 1));

CREATE INDEX IF NOT EXISTS idx_follows_followee_created
ON follows(followee_id, created_at DESC);
