package controller

import (
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/gin-gonic/gin"
)

func GetModelDeploymentSettings(c *gin.Context) {
	app.DeploymentHandlers().GetModelDeploymentSettings.Handle(c)
}

func TestIoNetConnection(c *gin.Context) { app.DeploymentHandlers().TestIoNetConnection.Handle(c) }

func GetAllDeployments(c *gin.Context) { app.DeploymentHandlers().GetAllDeployments.Handle(c) }

func SearchDeployments(c *gin.Context) { app.DeploymentHandlers().SearchDeployments.Handle(c) }

func GetDeployment(c *gin.Context) { app.DeploymentHandlers().GetDeployment.Handle(c) }

func UpdateDeploymentName(c *gin.Context) { app.DeploymentHandlers().UpdateDeploymentName.Handle(c) }

func UpdateDeployment(c *gin.Context) { app.DeploymentHandlers().UpdateDeployment.Handle(c) }

func ExtendDeployment(c *gin.Context) { app.DeploymentHandlers().ExtendDeployment.Handle(c) }

func DeleteDeployment(c *gin.Context) { app.DeploymentHandlers().DeleteDeployment.Handle(c) }

func CreateDeployment(c *gin.Context) { app.DeploymentHandlers().CreateDeployment.Handle(c) }

func GetHardwareTypes(c *gin.Context) { app.DeploymentHandlers().GetHardwareTypes.Handle(c) }

func GetLocations(c *gin.Context) { app.DeploymentHandlers().GetLocations.Handle(c) }

func GetAvailableReplicas(c *gin.Context) { app.DeploymentHandlers().GetAvailableReplicas.Handle(c) }

func GetPriceEstimation(c *gin.Context) { app.DeploymentHandlers().GetPriceEstimation.Handle(c) }

func CheckClusterNameAvailability(c *gin.Context) {
	app.DeploymentHandlers().CheckClusterNameAvailability.Handle(c)
}

func GetDeploymentLogs(c *gin.Context) { app.DeploymentHandlers().GetDeploymentLogs.Handle(c) }

func ListDeploymentContainers(c *gin.Context) {
	app.DeploymentHandlers().ListDeploymentContainers.Handle(c)
}

func GetContainerDetails(c *gin.Context) { app.DeploymentHandlers().GetContainerDetails.Handle(c) }
