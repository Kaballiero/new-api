package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// BRIDGE: GetModelDeploymentSettings serves existing router registrations; remove when router assembles deployments handlers directly.
func GetModelDeploymentSettings(c *gin.Context) {
	app.DeploymentHandlers().GetModelDeploymentSettings.Handle(c)
}

// BRIDGE: TestIoNetConnection serves existing router registrations; remove when router assembles deployments handlers directly.
func TestIoNetConnection(c *gin.Context) { app.DeploymentHandlers().TestIoNetConnection.Handle(c) }

// BRIDGE: GetAllDeployments serves existing router registrations; remove when router assembles deployments handlers directly.
func GetAllDeployments(c *gin.Context) { app.DeploymentHandlers().GetAllDeployments.Handle(c) }

// BRIDGE: SearchDeployments serves existing router registrations; remove when router assembles deployments handlers directly.
func SearchDeployments(c *gin.Context) { app.DeploymentHandlers().SearchDeployments.Handle(c) }

// BRIDGE: GetDeployment serves existing router registrations; remove when router assembles deployments handlers directly.
func GetDeployment(c *gin.Context) { app.DeploymentHandlers().GetDeployment.Handle(c) }

// BRIDGE: UpdateDeploymentName serves existing router registrations; remove when router assembles deployments handlers directly.
func UpdateDeploymentName(c *gin.Context) { app.DeploymentHandlers().UpdateDeploymentName.Handle(c) }

// BRIDGE: UpdateDeployment serves existing router registrations; remove when router assembles deployments handlers directly.
func UpdateDeployment(c *gin.Context) { app.DeploymentHandlers().UpdateDeployment.Handle(c) }

// BRIDGE: ExtendDeployment serves existing router registrations; remove when router assembles deployments handlers directly.
func ExtendDeployment(c *gin.Context) { app.DeploymentHandlers().ExtendDeployment.Handle(c) }

// BRIDGE: DeleteDeployment serves existing router registrations; remove when router assembles deployments handlers directly.
func DeleteDeployment(c *gin.Context) { app.DeploymentHandlers().DeleteDeployment.Handle(c) }

// BRIDGE: CreateDeployment serves existing router registrations; remove when router assembles deployments handlers directly.
func CreateDeployment(c *gin.Context) { app.DeploymentHandlers().CreateDeployment.Handle(c) }

// BRIDGE: GetHardwareTypes serves existing router registrations; remove when router assembles deployments handlers directly.
func GetHardwareTypes(c *gin.Context) { app.DeploymentHandlers().GetHardwareTypes.Handle(c) }

// BRIDGE: GetLocations serves existing router registrations; remove when router assembles deployments handlers directly.
func GetLocations(c *gin.Context) { app.DeploymentHandlers().GetLocations.Handle(c) }

// BRIDGE: GetAvailableReplicas serves existing router registrations; remove when router assembles deployments handlers directly.
func GetAvailableReplicas(c *gin.Context) { app.DeploymentHandlers().GetAvailableReplicas.Handle(c) }

// BRIDGE: GetPriceEstimation serves existing router registrations; remove when router assembles deployments handlers directly.
func GetPriceEstimation(c *gin.Context) { app.DeploymentHandlers().GetPriceEstimation.Handle(c) }

// BRIDGE: CheckClusterNameAvailability serves existing router registrations; remove when router assembles deployments handlers directly.
func CheckClusterNameAvailability(c *gin.Context) {
	app.DeploymentHandlers().CheckClusterNameAvailability.Handle(c)
}

// BRIDGE: GetDeploymentLogs serves existing router registrations; remove when router assembles deployments handlers directly.
func GetDeploymentLogs(c *gin.Context) { app.DeploymentHandlers().GetDeploymentLogs.Handle(c) }

// BRIDGE: ListDeploymentContainers serves existing router registrations; remove when router assembles deployments handlers directly.
func ListDeploymentContainers(c *gin.Context) {
	app.DeploymentHandlers().ListDeploymentContainers.Handle(c)
}

// BRIDGE: GetContainerDetails serves existing router registrations; remove when router assembles deployments handlers directly.
func GetContainerDetails(c *gin.Context) { app.DeploymentHandlers().GetContainerDetails.Handle(c) }
