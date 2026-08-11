package org

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/hanzoai/cek"
	"github.com/hanzoai/namespace"
)

// OrgStorage manages per-org and per-user S3 bucket isolation.
//
// Bucket layout on Hanzo S3 (s3.hanzo.space):
//
//	orgs/{orgSlug}/                         ← org bucket prefix
//	orgs/{orgSlug}/org/                     ← org-level shared data
//	orgs/{orgSlug}/users/{userId}/          ← per-user isolated storage
//
// Each org gets its own SSE-KMS key derived from the master key.
// Each user gets their own SSE-C key for client-side encryption.
//
// IAM policy ensures:
//   - Org admins can access orgs/{orgSlug}/*
//   - Users can only access orgs/{orgSlug}/users/{userId}/*
//   - No cross-org or cross-user access possible
type OrgStorage struct {
	// Endpoint is the S3 endpoint (e.g., "s3.hanzo.space:9000" or "s3.hanzo.ai").
	Endpoint string

	// Bucket is the root bucket name (e.g., "orgs").
	Bucket string

	// MasterKey for deriving per-org and per-user SSE keys.
	MasterKey string

	// UseSSL enables TLS for the S3 connection.
	UseSSL bool

	// Region for the S3 bucket (default: "us-east-1").
	Region string
}

// OrgPrefix returns the S3 key prefix for an org.
func (s *OrgStorage) OrgPrefix(orgSlug string) string {
	return fmt.Sprintf("orgs/%s/", orgSlug)
}

// OrgDataPrefix returns the S3 key prefix for org-level shared data.
func (s *OrgStorage) OrgDataPrefix(orgSlug string) string {
	return fmt.Sprintf("orgs/%s/org/", orgSlug)
}

// UserPrefix returns the S3 key prefix for a specific user.
func (s *OrgStorage) UserPrefix(orgSlug, userId string) string {
	return fmt.Sprintf("orgs/%s/users/%s/", orgSlug, userId)
}

// s3Subsystem is the store these keys encrypt. Which entity's objects they
// encrypt is the namespace's job, not the subsystem's, so one name serves both
// the org and the user key.
const s3Subsystem = "s3"

// sseKey derives ns's SSE-C key with github.com/hanzoai/cek — the same
// derivation as OrgDB's database keys, differing only in the subsystem, so an
// org's objects and its database do not share a key. Base64 because that is the
// encoding S3 wants in the SSE-C header; the key itself is the usual 32 bytes.
func (s *OrgStorage) sseKey(ns namespace.Namespace) (string, error) {
	if s.MasterKey == "" {
		return "", nil
	}
	key, err := cek.DeriveKey([]byte(s.MasterKey), ns, s3Subsystem)
	if err != nil {
		return "", fmt.Errorf("platform: derive s3 key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// OrgSSEKey derives a per-org SSE-C encryption key (32 bytes, base64-encoded).
// Used as the SSE-C CustomerKey header for server-side encryption.
func (s *OrgStorage) OrgSSEKey(orgSlug string) (string, error) {
	ns, err := namespace.Org(orgSlug)
	if err != nil {
		return "", err
	}
	return s.sseKey(ns)
}

// UserSSEKey derives a per-user SSE-C encryption key (32 bytes, base64-encoded).
// Each user's objects are encrypted with a unique key — even if the bucket is
// shared, objects cannot be decrypted without the user-specific key.
func (s *OrgStorage) UserSSEKey(orgSlug, userId string) (string, error) {
	ns, err := namespace.Of(orgSlug + "/" + userId)
	if err != nil {
		return "", err
	}
	return s.sseKey(ns)
}

// BucketPolicy returns a MinIO/S3 bucket policy JSON that enforces per-org
// path isolation. Uses encoding/json to prevent injection via orgSlug/iamUser.
func (s *OrgStorage) BucketPolicy(orgSlug, iamUser string) string {
	if err := validateSlug(orgSlug); err != nil {
		return "{}"
	}
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":    "Allow",
				"Principal": map[string]any{"AWS": []string{iamUser}},
				"Action":    []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"},
				"Resource":  []string{fmt.Sprintf("arn:aws:s3:::%s/orgs/%s/*", s.Bucket, orgSlug)},
				"Condition": map[string]any{
					"StringLike": map[string]any{
						"s3:prefix": []string{fmt.Sprintf("orgs/%s/*", orgSlug)},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(policy, "", "  ")
	return string(b)
}

// UserBucketPolicy returns a policy restricting a user to their own prefix only.
func (s *OrgStorage) UserBucketPolicy(orgSlug, userId, iamUser string) string {
	if err := validateSlug(orgSlug); err != nil {
		return "{}"
	}
	if err := validateSlug(userId); err != nil {
		return "{}"
	}
	prefix := fmt.Sprintf("orgs/%s/users/%s/*", orgSlug, userId)
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":    "Allow",
				"Principal": map[string]any{"AWS": []string{iamUser}},
				"Action":    []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"},
				"Resource":  []string{fmt.Sprintf("arn:aws:s3:::%s/%s", s.Bucket, prefix)},
			},
			{
				"Effect":    "Allow",
				"Principal": map[string]any{"AWS": []string{iamUser}},
				"Action":    []string{"s3:ListBucket"},
				"Resource":  []string{fmt.Sprintf("arn:aws:s3:::%s", s.Bucket)},
				"Condition": map[string]any{
					"StringLike": map[string]any{
						"s3:prefix": []string{prefix},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(policy, "", "  ")
	return string(b)
}
