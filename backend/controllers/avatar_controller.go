package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"arguehub/db"
	"arguehub/services"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AvatarController struct {
	storage services.AvatarStorage
}

func NewAvatarController(storage services.AvatarStorage) *AvatarController {
	return &AvatarController{storage: storage}
}

func (controller *AvatarController) PresignUpload(ctx *gin.Context) {
	if controller.storage == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Avatar uploads are not configured"})
		return
	}

	userID, ok := authenticatedUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var request struct {
		ContentType string `json:"contentType" binding:"required"`
		FileSize    int64  `json:"fileSize" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "contentType and fileSize are required"})
		return
	}

	upload, err := controller.storage.CreatePresignedUpload(
		ctx.Request.Context(),
		userID.Hex(),
		request.ContentType,
		request.FileSize,
	)
	if err != nil {
		if errors.Is(err, services.ErrInvalidAvatar) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("failed to presign avatar upload for user %s: %v", userID.Hex(), err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare avatar upload"})
		return
	}

	ctx.JSON(http.StatusOK, upload)
}

func (controller *AvatarController) ConfirmUpload(ctx *gin.Context) {
	if controller.storage == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Avatar uploads are not configured"})
		return
	}

	userID, ok := authenticatedUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var request struct {
		ObjectKey string `json:"objectKey" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "objectKey is required"})
		return
	}
	if !controller.storage.OwnsObject(userID.Hex(), request.ObjectKey) {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "Avatar object does not belong to this user"})
		return
	}

	operationCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	if err := controller.storage.ValidateUploadedObject(operationCtx, request.ObjectKey); err != nil {
		controller.deleteObject(request.ObjectKey, "rejected")
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Uploaded file is not a valid avatar"})
		return
	}
	avatarKey, err := controller.storage.PromoteUploadedObject(operationCtx, request.ObjectKey)
	if err != nil {
		controller.deleteObject(request.ObjectKey, "unconfirmed")
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to confirm avatar upload"})
		return
	}

	avatarURL := controller.storage.PublicURL(avatarKey)
	var previous struct {
		AvatarKey string `bson:"avatarKey"`
	}
	err = db.MongoDatabase.Collection("users").FindOneAndUpdate(
		operationCtx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{
			"avatarUrl": avatarURL,
			"avatarKey": avatarKey,
			"updatedAt": time.Now(),
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.Before).SetProjection(bson.M{"avatarKey": 1}),
	).Decode(&previous)
	if err != nil {
		controller.deleteObject(avatarKey, "unpersisted")
		if errors.Is(err, mongo.ErrNoDocuments) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		log.Printf("failed to persist avatar for user %s: %v", userID.Hex(), err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save avatar to profile"})
		return
	}

	if previous.AvatarKey != "" && previous.AvatarKey != avatarKey {
		controller.deleteObject(previous.AvatarKey, "previous")
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message":   "Avatar updated successfully",
		"avatarUrl": avatarURL,
	})
}

func (controller *AvatarController) SetGeneratedAvatar(ctx *gin.Context) {
	userID, ok := authenticatedUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var request struct {
		AvatarURL string `json:"avatarUrl" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil || !isAllowedGeneratedAvatarURL(request.AvatarURL) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Only HTTPS DiceBear avatar URLs are allowed"})
		return
	}

	operationCtx, cancel := context.WithTimeout(ctx.Request.Context(), 10*time.Second)
	defer cancel()

	var previous struct {
		AvatarKey string `bson:"avatarKey"`
	}
	err := db.MongoDatabase.Collection("users").FindOneAndUpdate(
		operationCtx,
		bson.M{"_id": userID},
		bson.M{
			"$set":   bson.M{"avatarUrl": request.AvatarURL, "updatedAt": time.Now()},
			"$unset": bson.M{"avatarKey": ""},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.Before).SetProjection(bson.M{"avatarKey": 1}),
	).Decode(&previous)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update avatar"})
		return
	}

	if controller.storage != nil && previous.AvatarKey != "" {
		controller.deleteObject(previous.AvatarKey, "previous")
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message":   "Avatar updated successfully",
		"avatarUrl": request.AvatarURL,
	})
}

func (controller *AvatarController) deleteObject(objectKey, reason string) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := controller.storage.DeleteObject(cleanupCtx, objectKey); err != nil {
		log.Printf("failed to delete %s avatar object %s: %v", reason, objectKey, err)
	}
}

func authenticatedUserID(ctx *gin.Context) (primitive.ObjectID, bool) {
	value, exists := ctx.Get("userID")
	if !exists {
		return primitive.NilObjectID, false
	}
	userID, ok := value.(primitive.ObjectID)
	return userID, ok && !userID.IsZero()
}

func isAllowedGeneratedAvatarURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() == "api.dicebear.com"
}
