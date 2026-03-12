package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func setUserRole(t *testing.T, database *sql.DB, userID, role string) {
	t.Helper()
	if _, err := database.Exec(`UPDATE users SET role = ?, updated_at = datetime('now') WHERE id = ?`, role, userID); err != nil {
		t.Fatalf("set user role: %v", err)
	}
}

func TestAdminEndpointPermission(t *testing.T) {
	srv, container := newTestServer(t)
	userID, accessToken, _ := registerUser(t, srv, "adminish", "adminish@example.com", "password123")

	status, _ := doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/overview", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("admin endpoint without token should return 401, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/overview", nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if status != http.StatusForbidden {
		t.Fatalf("non-admin token should return 403, got %d", status)
	}

	setUserRole(t, container.DB, userID, "admin")
	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/overview", nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if status != http.StatusOK {
		t.Fatalf("admin token should return 200, got %d", status)
	}
}

func TestAdminWriteActionCreatesAuditLog(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "boss", "boss@example.com", "password123")
	targetID, _, _ := registerUser(t, srv, "target", "target@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	status, _ := doJSONRequest(t, srv, http.MethodPatch, fmt.Sprintf("/api/v1/admin/users/%s", targetID), map[string]interface{}{
		"status": "disabled",
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusOK {
		t.Fatalf("admin patch user should return 200, got %d", status)
	}

	var userStatus string
	if err := container.DB.QueryRow(`SELECT status FROM users WHERE id = ?`, targetID).Scan(&userStatus); err != nil {
		t.Fatalf("query user status: %v", err)
	}
	if userStatus != "disabled" {
		t.Fatalf("expected target status disabled, got %s", userStatus)
	}

	status, logsResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/audit-logs?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("admin list audit logs should return 200, got %d", status)
	}

	var payload struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	if err := json.Unmarshal(logsResp.Data, &payload); err != nil {
		t.Fatalf("parse audit logs response: %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatalf("expected audit logs items")
	}

	found := false
	for _, item := range payload.Items {
		if item.Action == "user.patch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected action user.patch in audit logs")
	}
}
