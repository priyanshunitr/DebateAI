package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type recordingS3Client struct {
	copyInput   *s3.CopyObjectInput
	deleteInput *s3.DeleteObjectInput
}

func (client *recordingS3Client) HeadObject(
	context.Context,
	*s3.HeadObjectInput,
	...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	panic("unexpected HeadObject call")
}

func (client *recordingS3Client) GetObject(
	context.Context,
	*s3.GetObjectInput,
	...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	panic("unexpected GetObject call")
}

func (client *recordingS3Client) CopyObject(
	_ context.Context,
	input *s3.CopyObjectInput,
	_ ...func(*s3.Options),
) (*s3.CopyObjectOutput, error) {
	client.copyInput = input
	return &s3.CopyObjectOutput{}, nil
}

func (client *recordingS3Client) DeleteObject(
	_ context.Context,
	input *s3.DeleteObjectInput,
	_ ...func(*s3.Options),
) (*s3.DeleteObjectOutput, error) {
	client.deleteInput = input
	return &s3.DeleteObjectOutput{}, nil
}

func TestNormalizeContentType(t *testing.T) {
	if got := normalizeContentType(" Image/PNG; charset=binary "); got != "image/png" {
		t.Fatalf("normalizeContentType() = %q, want image/png", got)
	}
}

func TestOwnsObject(t *testing.T) {
	storage := &S3AvatarStorage{}
	tests := []struct {
		name      string
		userID    string
		objectKey string
		want      bool
	}{
		{name: "owned upload", userID: "abc", objectKey: "avatar-uploads/abc/photo.png", want: true},
		{name: "another user", userID: "abc", objectKey: "avatar-uploads/def/photo.png", want: false},
		{name: "published object", userID: "abc", objectKey: "avatars/abc/photo.png", want: false},
		{name: "path traversal", userID: "abc", objectKey: "avatar-uploads/abc/../def/photo.png", want: false},
		{name: "backslash", userID: "abc", objectKey: "avatar-uploads/abc\\photo.png", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := storage.OwnsObject(test.userID, test.objectKey); got != test.want {
				t.Fatalf("OwnsObject() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPublicURL(t *testing.T) {
	storage := &S3AvatarStorage{publicBaseURL: "https://cdn.example.com"}
	if got := storage.PublicURL("avatars/user id/photo.png"); got != "https://cdn.example.com/avatars/user%20id/photo.png" {
		t.Fatalf("PublicURL() = %q", got)
	}
}

func TestCreatePresignedUpload(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "ap-south-1",
		Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""),
	})
	storage := &S3AvatarStorage{
		bucket:        "avatar-bucket",
		publicBaseURL: "https://avatars.example.com",
		presignTTL:    fiveMinutes,
		client:        client,
		presigner:     s3.NewPresignClient(client),
	}

	upload, err := storage.CreatePresignedUpload(context.Background(), "user123", "image/png", 1024)
	if err != nil {
		t.Fatalf("CreatePresignedUpload() error = %v", err)
	}
	if !storage.OwnsObject("user123", upload.ObjectKey) || !strings.HasSuffix(upload.ObjectKey, ".png") {
		t.Fatalf("unexpected object key %q", upload.ObjectKey)
	}
	if upload.Headers["Content-Type"] != "image/png" ||
		upload.Headers["x-amz-tagging"] != pendingUploadTag ||
		upload.Headers["Cache-Control"] != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected upload headers: %#v", upload.Headers)
	}
	if !strings.Contains(upload.UploadURL, "X-Amz-Signature=") {
		t.Fatalf("upload URL is not presigned: %q", upload.UploadURL)
	}
}

func TestPromoteUploadedObject(t *testing.T) {
	client := &recordingS3Client{}
	storage := &S3AvatarStorage{bucket: "avatar-bucket", client: client}
	uploadKey := "avatar-uploads/user123/photo.png"

	avatarKey, err := storage.PromoteUploadedObject(context.Background(), uploadKey)
	if err != nil {
		t.Fatalf("PromoteUploadedObject() error = %v", err)
	}
	if avatarKey != "avatars/user123/photo.png" {
		t.Fatalf("PromoteUploadedObject() key = %q", avatarKey)
	}
	if client.copyInput == nil || aws.ToString(client.copyInput.Key) != avatarKey {
		t.Fatalf("unexpected copy input: %#v", client.copyInput)
	}
	if aws.ToString(client.copyInput.CopySource) != "avatar-bucket%2Favatar-uploads%2Fuser123%2Fphoto.png" {
		t.Fatalf("unexpected copy source: %q", aws.ToString(client.copyInput.CopySource))
	}
	if aws.ToString(client.copyInput.Tagging) != "upload-state=confirmed" {
		t.Fatalf("unexpected copy tagging: %q", aws.ToString(client.copyInput.Tagging))
	}
	if client.deleteInput == nil || aws.ToString(client.deleteInput.Key) != uploadKey {
		t.Fatalf("temporary upload was not deleted: %#v", client.deleteInput)
	}
}

const fiveMinutes = 5 * time.Minute
