package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	appconfig "arguehub/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

const (
	MaxAvatarSize      int64 = 5 << 20
	avatarObjectPrefix       = "avatars"
	avatarUploadPrefix       = "avatar-uploads"
	pendingUploadTag         = "upload-state=pending"
)

var ErrInvalidAvatar = errors.New("invalid avatar")

var avatarExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

type PresignedAvatarUpload struct {
	UploadURL string            `json:"uploadUrl"`
	ObjectKey string            `json:"objectKey"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

type AvatarStorage interface {
	CreatePresignedUpload(ctx context.Context, userID, contentType string, fileSize int64) (*PresignedAvatarUpload, error)
	ValidateUploadedObject(ctx context.Context, objectKey string) (string, error)
	PromoteUploadedObject(ctx context.Context, objectKey, sourceETag string) (string, error)
	DeleteObject(ctx context.Context, objectKey string) error
	PublicURL(objectKey string) string
	OwnsObject(userID, objectKey string) bool
}

type s3ObjectClient interface {
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type S3AvatarStorage struct {
	bucket     string
	region     string
	presignTTL time.Duration
	client     s3ObjectClient
	presigner  *s3.PresignClient
}

func NewS3AvatarStorage(ctx context.Context, cfg appconfig.S3Config) (*S3AvatarStorage, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("S3 avatar storage requires region and bucket")
	}
	if strings.Contains(cfg.Bucket, ".") {
		return nil, fmt.Errorf("direct public S3 avatar bucket name must not contain dots")
	}
	if cfg.PresignTTLSeconds < 60 || cfg.PresignTTLSeconds > 900 {
		return nil, fmt.Errorf("S3 presignTTLSeconds must be between 60 and 900")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)
	return &S3AvatarStorage{
		bucket:     cfg.Bucket,
		region:     cfg.Region,
		presignTTL: time.Duration(cfg.PresignTTLSeconds) * time.Second,
		client:     client,
		presigner:  s3.NewPresignClient(client),
	}, nil
}

func (s *S3AvatarStorage) CreatePresignedUpload(
	ctx context.Context,
	userID string,
	contentType string,
	fileSize int64,
) (*PresignedAvatarUpload, error) {
	contentType = normalizeContentType(contentType)
	extension, ok := avatarExtensions[contentType]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported content type", ErrInvalidAvatar)
	}
	if fileSize <= 0 || fileSize > MaxAvatarSize {
		return nil, fmt.Errorf("%w: file must be between 1 byte and %d bytes", ErrInvalidAvatar, MaxAvatarSize)
	}

	objectKey := fmt.Sprintf("%s/%s/%s%s", avatarUploadPrefix, userID, uuid.NewString(), extension)
	presigned, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		CacheControl:  aws.String("public, max-age=31536000, immutable"),
		Key:           aws.String(objectKey),
		ContentLength: aws.Int64(fileSize),
		ContentType:   aws.String(contentType),
		Tagging:       aws.String(pendingUploadTag),
	}, func(options *s3.PresignOptions) {
		options.Expires = s.presignTTL
	})
	if err != nil {
		return nil, fmt.Errorf("presign avatar upload: %w", err)
	}

	return &PresignedAvatarUpload{
		UploadURL: presigned.URL,
		ObjectKey: objectKey,
		Headers: map[string]string{
			"Cache-Control": "public, max-age=31536000, immutable",
			"Content-Type":  contentType,
			"x-amz-tagging": pendingUploadTag,
		},
		ExpiresAt: time.Now().Add(s.presignTTL).UTC(),
	}, nil
}

func (s *S3AvatarStorage) ValidateUploadedObject(ctx context.Context, objectKey string) (string, error) {
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return "", fmt.Errorf("inspect uploaded avatar: %w", err)
	}
	if head.ContentLength == nil || *head.ContentLength <= 0 || *head.ContentLength > MaxAvatarSize {
		return "", fmt.Errorf("uploaded avatar has an invalid size")
	}
	if aws.ToString(head.ETag) == "" {
		return "", fmt.Errorf("uploaded avatar has no entity tag")
	}

	declaredType := normalizeContentType(aws.ToString(head.ContentType))
	expectedExtension, ok := avatarExtensions[declaredType]
	if !ok || !strings.EqualFold(path.Ext(objectKey), expectedExtension) {
		return "", fmt.Errorf("uploaded avatar has an invalid content type")
	}

	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:  aws.String(s.bucket),
		IfMatch: head.ETag,
		Key:     aws.String(objectKey),
		Range:   aws.String("bytes=0-511"),
	})
	if err != nil {
		return "", fmt.Errorf("read uploaded avatar signature: %w", err)
	}
	defer object.Body.Close()

	signature, err := io.ReadAll(io.LimitReader(object.Body, 512))
	if err != nil {
		return "", fmt.Errorf("read uploaded avatar signature: %w", err)
	}
	if len(signature) == 0 || normalizeContentType(http.DetectContentType(signature)) != declaredType {
		return "", fmt.Errorf("uploaded file content does not match its image type")
	}

	return aws.ToString(head.ETag), nil
}

func (s *S3AvatarStorage) PromoteUploadedObject(ctx context.Context, uploadKey, sourceETag string) (string, error) {
	avatarKey := strings.Replace(uploadKey, avatarUploadPrefix+"/", avatarObjectPrefix+"/", 1)
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(s.bucket),
		CopySource:        aws.String(url.PathEscape(s.bucket + "/" + uploadKey)),
		CopySourceIfMatch: aws.String(sourceETag),
		Key:               aws.String(avatarKey),
		Tagging:           aws.String("upload-state=confirmed"),
		TaggingDirective:  s3types.TaggingDirectiveReplace,
	})
	if err != nil {
		return "", fmt.Errorf("promote avatar object: %w", err)
	}

	if err := s.DeleteObject(ctx, uploadKey); err != nil {
		// The pending-object lifecycle rule will remove this source object later.
		log.Printf("failed to delete promoted avatar upload %s: %v", uploadKey, err)
	}
	return avatarKey, nil
}

func (s *S3AvatarStorage) DeleteObject(ctx context.Context, objectKey string) error {
	if objectKey == "" {
		return nil
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("delete avatar object: %w", err)
	}
	return nil
}

func (s *S3AvatarStorage) PublicURL(objectKey string) string {
	segments := strings.Split(objectKey, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return fmt.Sprintf(
		"https://%s.s3.%s.amazonaws.com/%s",
		s.bucket,
		s.region,
		strings.Join(segments, "/"),
	)
}

func (s *S3AvatarStorage) OwnsObject(userID, objectKey string) bool {
	expectedPrefix := fmt.Sprintf("%s/%s/", avatarUploadPrefix, userID)
	return objectKey == path.Clean(objectKey) &&
		!strings.Contains(objectKey, "\\") &&
		strings.HasPrefix(objectKey, expectedPrefix)
}

func normalizeContentType(contentType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}
