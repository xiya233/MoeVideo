package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
)

var errVideoDeleteNotFound = errors.New("video not found")

type deleteStorageObject struct {
	Provider  string
	Bucket    string
	ObjectKey string
}

type videoDeletePlan struct {
	VideoID       string
	UploaderID    string
	BeforeStatus  string
	HLSProvider   string
	HLSBucket     string
	HLSPrefix     string
	StorageObject []deleteStorageObject
}

func (h *Handler) deleteVideoInTx(
	ctx context.Context,
	tx *sql.Tx,
	videoID string,
	requesterID string,
	requireOwner bool,
) (*videoDeletePlan, error) {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil, errVideoDeleteNotFound
	}

	var (
		uploaderID     string
		beforeStatus   string
		coverMediaID   sql.NullString
		sourceMediaID  sql.NullString
		previewMediaID sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
SELECT uploader_id, status, cover_media_id, source_media_id, preview_media_id
FROM videos
WHERE id = ?
LIMIT 1`,
		videoID,
	).Scan(&uploaderID, &beforeStatus, &coverMediaID, &sourceMediaID, &previewMediaID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errVideoDeleteNotFound
		}
		return nil, fmt.Errorf("query video before delete: %w", err)
	}
	if requireOwner && strings.TrimSpace(uploaderID) != strings.TrimSpace(requesterID) {
		return nil, errVideoDeleteNotFound
	}

	plan := &videoDeletePlan{
		VideoID:      videoID,
		UploaderID:   uploaderID,
		BeforeStatus: beforeStatus,
	}

	var (
		hlsProvider sql.NullString
		hlsBucket   sql.NullString
		hlsMaster   sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
SELECT provider, COALESCE(bucket, ''), master_object_key
FROM video_hls_assets
WHERE video_id = ?
LIMIT 1`,
		videoID,
	).Scan(&hlsProvider, &hlsBucket, &hlsMaster); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query video hls assets before delete: %w", err)
	}
	if strings.TrimSpace(hlsMaster.String) != "" {
		plan.HLSProvider = strings.TrimSpace(hlsProvider.String)
		plan.HLSBucket = strings.TrimSpace(hlsBucket.String)
		plan.HLSPrefix = objectPrefixFromMaster(strings.TrimSpace(hlsMaster.String))
	}

	now := nowString()
	if _, err := tx.ExecContext(ctx, `UPDATE video_import_items SET video_id = NULL, media_object_id = NULL, updated_at = ? WHERE video_id = ?`, now, videoID); err != nil {
		return nil, fmt.Errorf("clear import item references: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM site_featured_banners WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete featured banners: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comment_likes WHERE comment_id IN (SELECT id FROM comments WHERE video_id = ?)`, videoID); err != nil {
		return nil, fmt.Errorf("delete comment likes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete comments: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_tags WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video tags: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_actions WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video actions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_view_events WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video views: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_video_progress WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete user video progress: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_danmaku WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video danmaku: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_transcode_jobs WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video transcode jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_hls_assets WHERE video_id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video hls assets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM videos WHERE id = ?`, videoID); err != nil {
		return nil, fmt.Errorf("delete video row: %w", err)
	}

	mediaIDs := uniqueNonEmptyStrings(
		coverMediaID.String,
		sourceMediaID.String,
		previewMediaID.String,
	)
	for _, mediaID := range mediaIDs {
		obj, err := h.fetchMediaObjectTx(ctx, tx, mediaID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("query media object %s: %w", mediaID, err)
		}
		used, err := h.isMediaObjectReferencedTx(ctx, tx, mediaID)
		if err != nil {
			return nil, fmt.Errorf("check media object references %s: %w", mediaID, err)
		}
		if used {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_objects WHERE id = ?`, mediaID); err != nil {
			return nil, fmt.Errorf("delete media object %s: %w", mediaID, err)
		}
		plan.StorageObject = append(plan.StorageObject, *obj)
	}

	return plan, nil
}

func (h *Handler) fetchMediaObjectTx(ctx context.Context, tx *sql.Tx, mediaID string) (*deleteStorageObject, error) {
	row := tx.QueryRowContext(ctx, `
SELECT provider, COALESCE(bucket, ''), object_key
FROM media_objects
WHERE id = ?
LIMIT 1`,
		mediaID,
	)
	var (
		provider  string
		bucket    string
		objectKey string
	)
	if err := row.Scan(&provider, &bucket, &objectKey); err != nil {
		return nil, err
	}
	return &deleteStorageObject{
		Provider:  strings.TrimSpace(provider),
		Bucket:    strings.TrimSpace(bucket),
		ObjectKey: strings.TrimSpace(objectKey),
	}, nil
}

func (h *Handler) isMediaObjectReferencedTx(ctx context.Context, tx *sql.Tx, mediaID string) (bool, error) {
	var exists int64
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM videos WHERE cover_media_id = ? OR preview_media_id = ? OR source_media_id = ?
	UNION ALL
	SELECT 1 FROM users WHERE avatar_media_id = ?
	UNION ALL
	SELECT 1 FROM site_settings WHERE site_logo_media_id = ?
	UNION ALL
	SELECT 1 FROM upload_sessions WHERE media_object_id = ?
	UNION ALL
	SELECT 1 FROM video_import_items WHERE media_object_id = ?
)`,
		mediaID,
		mediaID,
		mediaID,
		mediaID,
		mediaID,
		mediaID,
		mediaID,
	).Scan(&exists); err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (h *Handler) cleanupVideoStorage(ctx context.Context, plan *videoDeletePlan) []string {
	if plan == nil {
		return nil
	}

	warnings := make([]string, 0)
	if strings.TrimSpace(plan.HLSPrefix) != "" {
		if err := h.app.Storage.DeletePrefix(ctx, plan.HLSProvider, plan.HLSBucket, plan.HLSPrefix); err != nil {
			msg := fmt.Sprintf("delete_hls_prefix_failed prefix=%s err=%v", plan.HLSPrefix, err)
			warnings = append(warnings, msg)
			log.Printf("module=video_delete level=warn video_id=%s %s", plan.VideoID, msg)
		}
	}

	seen := make(map[string]struct{}, len(plan.StorageObject))
	for _, item := range plan.StorageObject {
		key := strings.ToLower(strings.TrimSpace(item.Provider + "|" + item.Bucket + "|" + item.ObjectKey))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := h.app.Storage.DeleteObject(ctx, item.Provider, item.Bucket, item.ObjectKey); err != nil {
			msg := fmt.Sprintf("delete_object_failed object_key=%s err=%v", item.ObjectKey, err)
			warnings = append(warnings, msg)
			log.Printf("module=video_delete level=warn video_id=%s %s", plan.VideoID, msg)
		}
	}

	return warnings
}

func objectPrefixFromMaster(masterObjectKey string) string {
	key := strings.TrimSpace(masterObjectKey)
	if key == "" {
		return ""
	}
	dir := path.Dir(key)
	if dir == "." || dir == "/" {
		return ""
	}
	return strings.TrimPrefix(dir, "/")
}

func uniqueNonEmptyStrings(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
