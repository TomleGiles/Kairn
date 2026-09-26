// Package objstore stocke les exports et rapports (S3-compatible en
// production — OVH, Scaleway, MinIO —, système de fichiers local en démo).
// Les clés sont toujours préfixées par l'organisation.
package objstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound signale un objet absent.
var ErrNotFound = errors.New("objstore: not found")

// Store est un stockage objet minimal.
type Store interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	// DeletePrefix supprime tous les objets dont la clé commence par prefix (ex. Key(org)).
	DeletePrefix(ctx context.Context, prefix string) error
}

// Key construit une clé d'objet sûre préfixée par l'organisation.
func Key(orgID string, parts ...string) string {
	clean := []string{"orgs", orgID}
	for _, p := range parts {
		p = strings.ReplaceAll(p, "..", "")
		p = strings.Trim(p, "/")
		if p != "" {
			clean = append(clean, p)
		}
	}
	return strings.Join(clean, "/")
}

// Local stocke les objets sur disque.
type Local struct{ Dir string }

func (l Local) path(key string) (string, error) {
	p := filepath.Join(l.Dir, filepath.FromSlash(key))
	root, _ := filepath.Abs(l.Dir)
	abs, _ := filepath.Abs(p)
	if !strings.HasPrefix(abs, root) {
		return "", fmt.Errorf("objstore: invalid key %q", key)
	}
	return p, nil
}

// Put écrit un objet.
func (l Local) Put(_ context.Context, key string, data []byte, _ string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Get lit un objet.
func (l Local) Get(_ context.Context, key string) ([]byte, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// Delete supprime un objet.
func (l Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// S3 stocke les objets dans un bucket S3-compatible.
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 crée un client S3-compatible.
func NewS3(endpoint, accessKey, secretKey, bucket, region string, useSSL bool) (*S3, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: useSSL, Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("objstore: s3 client: %w", err)
	}
	return &S3{client: c, bucket: bucket}, nil
}

// EnsureBucket crée le bucket s'il n'existe pas.
func (s *S3) EnsureBucket(ctx context.Context, region string) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !ok {
		return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region})
	}
	return nil
}

// Put écrit un objet (chiffrement côté serveur si le fournisseur le propose).
func (s *S3) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Get lit un objet.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = obj.Close() }()
	b, err := io.ReadAll(obj)
	if err != nil {
		var resp minio.ErrorResponse
		if errors.As(err, &resp) && resp.Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

// Delete supprime un objet.
func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// DeletePrefix supprime une arborescence locale.
func (l Local) DeletePrefix(_ context.Context, prefix string) error {
	if strings.Trim(prefix, "/") == "" {
		return fmt.Errorf("objstore: refusing to delete the whole store")
	}
	p, err := l.path(prefix)
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}

// DeletePrefix supprime tous les objets d'un préfixe S3.
func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	if strings.Trim(prefix, "/") == "" {
		return fmt.Errorf("objstore: refusing to delete the whole bucket")
	}
	objects := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: strings.TrimSuffix(prefix, "/") + "/", Recursive: true})
	for err := range s.client.RemoveObjects(ctx, s.bucket, objects, minio.RemoveObjectsOptions{}) {
		if err.Err != nil {
			return fmt.Errorf("objstore: delete %s: %w", err.ObjectName, err.Err)
		}
	}
	return nil
}
