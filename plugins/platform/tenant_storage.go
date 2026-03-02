package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// TenantStorage manages per-org and per-user S3 bucket isolation.
//
// Bucket layout on Hanzo S3 (s3.hanzo.space):
//
//	tenants/{orgSlug}/                         ← org bucket prefix
//	tenants/{orgSlug}/org/                     ← org-level shared data
//	tenants/{orgSlug}/users/{userId}/          ← per-user isolated storage
//
// Each org gets its own SSE-KMS key derived from the master key.
// Each user gets their own SSE-C key for client-side encryption.
//
// IAM policy ensures:
//   - Org admins can access tenants/{orgSlug}/*
//   - Users can only access tenants/{orgSlug}/users/{userId}/*
//   - No cross-org or cross-user access possible
type TenantStorage struct {
	// Endpoint is the S3 endpoint (e.g., "s3.hanzo.space:9000" or "s3.hanzo.ai").
	Endpoint string

	// Bucket is the root bucket name (e.g., "tenants").
	Bucket string

	// MasterKey for deriving per-org and per-user SSE keys.
	MasterKey string

	// UseSSL enables TLS for the S3 connection.
	UseSSL bool

	// Region for the S3 bucket (default: "us-east-1").
	Region string
}

// OrgPrefix returns the S3 key prefix for an org.
func (s *TenantStorage) OrgPrefix(orgSlug string) string {
	return fmt.Sprintf("tenants/%s/", orgSlug)
}

// OrgDataPrefix returns the S3 key prefix for org-level shared data.
func (s *TenantStorage) OrgDataPrefix(orgSlug string) string {
	return fmt.Sprintf("tenants/%s/org/", orgSlug)
}

// UserPrefix returns the S3 key prefix for a specific user.
func (s *TenantStorage) UserPrefix(orgSlug, userId string) string {
	return fmt.Sprintf("tenants/%s/users/%s/", orgSlug, userId)
}

// OrgSSEKey derives a per-org SSE-C encryption key (32 bytes, base64-encoded).
// Used as the SSE-C CustomerKey header for server-side encryption.
func (s *TenantStorage) OrgSSEKey(orgSlug string) string {
	if s.MasterKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.MasterKey))
	mac.Write([]byte("s3:org:" + orgSlug))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// UserSSEKey derives a per-user SSE-C encryption key (32 bytes, base64-encoded).
// Each user's objects are encrypted with a unique key — even if the bucket is shared,
// objects cannot be decrypted without the user-specific key.
func (s *TenantStorage) UserSSEKey(orgSlug, userId string) string {
	if s.MasterKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.MasterKey))
	mac.Write([]byte("s3:user:" + orgSlug + ":" + userId))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// BucketPolicy returns a MinIO/S3 bucket policy JSON that enforces per-org
// and per-user path isolation. Each IAM user is constrained to their own prefix.
func (s *TenantStorage) BucketPolicy(orgSlug, iamUser string) string {
	return fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"AWS": ["%s"]},
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"],
      "Resource": [
        "arn:aws:s3:::%s/tenants/%s/*"
      ],
      "Condition": {
        "StringLike": {
          "s3:prefix": ["tenants/%s/*"]
        }
      }
    }
  ]
}`, iamUser, s.Bucket, orgSlug, orgSlug)
}

// UserBucketPolicy returns a policy restricting a user to their own prefix only.
func (s *TenantStorage) UserBucketPolicy(orgSlug, userId, iamUser string) string {
	return fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"AWS": ["%s"]},
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": [
        "arn:aws:s3:::%s/tenants/%s/users/%s/*"
      ]
    },
    {
      "Effect": "Allow",
      "Principal": {"AWS": ["%s"]},
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::%s"],
      "Condition": {
        "StringLike": {
          "s3:prefix": ["tenants/%s/users/%s/*"]
        }
      }
    }
  ]
}`, iamUser, s.Bucket, orgSlug, userId, iamUser, s.Bucket, orgSlug, userId)
}
