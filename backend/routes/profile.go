package routes

import (
	"arguehub/controllers"

	"github.com/gin-gonic/gin"
)

func GetProfileRouteHandler(ctx *gin.Context) {
	controllers.GetProfile(ctx)
}

func UpdateProfileRouteHandler(ctx *gin.Context) {
	controllers.UpdateProfile(ctx)
}

func CheckDisplayNameRouteHandler(ctx *gin.Context) {
	controllers.CheckDisplayName(ctx)
}

func UpdateEloAfterDebateRouteHandler(ctx *gin.Context) {
	controllers.UpdateEloAfterDebate(ctx)
}

func SetupAvatarRoutes(group *gin.RouterGroup, controller *controllers.AvatarController) {
	group.POST("/user/avatar-upload/presign", controller.PresignUpload)
	group.POST("/user/avatar-upload/confirm", controller.ConfirmUpload)
	group.PUT("/user/avatar", controller.SetGeneratedAvatar)
}
