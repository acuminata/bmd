package s3store

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config holds S3-compatible storage settings.
type Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string
	PathStyle bool
	PublicURL string
}

// Enabled reports whether uploads are configured.
func (c Config) Enabled() bool { return c.Bucket != "" && c.Endpoint != "" }

// Uploader puts original photos into an S3-compatible bucket.
type Uploader struct {
	cfg    Config
	client *s3.Client
}

func resolveRegion(cfg Config) string {
	if cfg.Region != "" {
		return cfg.Region
	}
	return "us-east-1"
}

// New builds an uploader; nil means photo uploads are disabled.
func New(cfg Config) (*Uploader, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	awsCfg := aws.Config{
		Region:      resolveRegion(cfg),
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		BaseEndpoint: &endpoint,
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.PathStyle
	})
	return &Uploader{cfg: cfg, client: client}, nil
}

// KeyFor builds the object key for an original photo:
//
//	{prefix}/{kind}/{application_number}/{slot}_original{ext}
func (u *Uploader) KeyFor(kind, applicationNumber, slot, photoPath, contentType string) string {
	ext := extFrom(contentType, photoPath)
	slot = strings.ToLower(strings.TrimSpace(slot))
	if slot != "groom" && slot != "bride" {
		slot = "photo"
	}
	prefix := strings.Trim(u.cfg.Prefix, "/")
	parts := []string{}
	if prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, strings.Trim(kind, "/"), sanitize(applicationNumber), fmt.Sprintf("%s_original%s", slot, ext))
	return path.Join(parts...)
}

// Put uploads bytes and returns the stored key and a URL for the DB.
func (u *Uploader) Put(ctx context.Context, key string, data []byte, contentType string) (storedURL string, err error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(u.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", err
	}
	if base := strings.TrimRight(u.cfg.PublicURL, "/"); base != "" {
		return base + "/" + strings.TrimLeft(key, "/"), nil
	}
	return fmt.Sprintf("s3://%s/%s", u.cfg.Bucket, key), nil
}

func extFrom(contentType, photoPath string) string {
	if contentType != "" {
		ct := strings.Split(contentType, ";")[0]
		ct = strings.TrimSpace(strings.ToLower(ct))
		if !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "json") && !strings.Contains(ct, "html") {
			if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
				return exts[0]
			}
		}
		switch ct {
		case "image/jpeg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/webp":
			return ".webp"
		case "image/gif":
			return ".gif"
		}
	}
	// Fall back to the ?name=... filename hint in "/files/download?name=X.jpeg&path=...".
	if i := strings.Index(photoPath, "name="); i >= 0 {
		name := photoPath[i+len("name="):]
		if j := strings.Index(name, "&"); j >= 0 {
			name = name[:j]
		}
		if un, err := url.QueryUnescape(name); err == nil {
			name = un
		}
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			ext := strings.ToLower(name[dot:])
			if len(ext) <= 5 {
				return ext
			}
		}
	}
	return ".jpg"
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	return s
}
