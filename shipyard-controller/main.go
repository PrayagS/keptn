package main

import (
	"github.com/gin-gonic/gin"
	"github.com/keptn/go-utils/pkg/common/osutils"
	keptncommon "github.com/keptn/go-utils/pkg/lib/keptn"
	"github.com/keptn/go-utils/pkg/lib/v0_2_0"
	"github.com/keptn/keptn/shipyard-controller/common"
	"github.com/keptn/keptn/shipyard-controller/controller"
	"github.com/keptn/keptn/shipyard-controller/db"
	"github.com/keptn/keptn/shipyard-controller/docs"
	"github.com/keptn/keptn/shipyard-controller/handler"
	"github.com/keptn/keptn/shipyard-controller/handler/sequencehooks"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strconv"
	"time"
)

// @title Control Plane API
// @version 1.0
// @description This is the API documentation of the Shipyard Controller.

// @securityDefinitions.apiKey ApiKeyAuth
// @in header
// @name x-token

// @contact.name Keptn Team
// @contact.url http://www.keptn.sh

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @BasePath /v1

const envVarConfigurationSvcEndpoint = "CONFIGURATION_SERVICE"
const envVarEventDispatchIntervalSec = "EVENT_DISPATCH_INTERVAL_SEC"
const envVarEventDispatchIntervalSecDefault = "10"

func main() {

	log.SetLevel(log.InfoLevel)

	if osutils.GetAndCompareOSEnv("GIN_MODE", "release") {
		docs.SwaggerInfo.Version = osutils.GetOSEnv("version")
		docs.SwaggerInfo.BasePath = "/api/shipyard-controller/v1"
		docs.SwaggerInfo.Schemes = []string{"https"}
	}

	eventDispatcherSyncInterval, err := strconv.Atoi(osutils.GetOSEnvOrDefault(envVarEventDispatchIntervalSec, envVarEventDispatchIntervalSecDefault))
	if err != nil {
		log.Fatalf("Unexpected value of EVENT_DISPATCH_INTERVAL_SEC environment variable. Need to be a number")
	}

	csEndpoint, err := keptncommon.GetServiceEndpoint(envVarConfigurationSvcEndpoint)
	if err != nil {
		log.Fatalf("could not get configuration-service URL: %s", err.Error())
	}

	kubeAPI, err := createKubeAPI()
	if err != nil {
		log.Fatalf("could not create kubernetes client: %s", err.Error())
	}

	eventSender, err := v0_2_0.NewHTTPEventSender("")
	if err != nil {
		log.Fatal(err)
	}

	projectManager := handler.NewProjectManager(
		common.NewGitConfigurationStore(csEndpoint.String()),
		createSecretStore(kubeAPI),
		createMaterializedView(),
		createTaskSequenceRepo(),
		createEventsRepo())

	serviceManager := handler.NewServiceManager(
		createMaterializedView(),
		common.NewGitConfigurationStore(csEndpoint.String()),
	)

	stageManager := handler.NewStageManager(createMaterializedView())

	eventDispatcher := handler.NewEventDispatcher(createEventsRepo(), createEventQueueRepo(), eventSender, time.Duration(eventDispatcherSyncInterval)*time.Second)
	shipyardController := handler.GetShipyardControllerInstance(eventDispatcher)

	engine := gin.Default()
	apiV1 := engine.Group("/v1")
	projectService := handler.NewProjectHandler(projectManager, eventSender)
	projectController := controller.NewProjectController(projectService)
	projectController.Inject(apiV1)

	serviceHandler := handler.NewServiceHandler(serviceManager, eventSender)
	serviceController := controller.NewServiceController(serviceHandler)
	serviceController.Inject(apiV1)

	eventHandler := handler.NewEventHandler(shipyardController)
	eventController := controller.NewEventController(eventHandler)
	eventController.Inject(apiV1)

	stageHandler := handler.NewStageHandler(stageManager)
	stageController := controller.NewStageController(stageHandler)
	stageController.Inject(apiV1)

	evaluationManager, err := handler.NewEvaluationManager(eventSender, createMaterializedView())
	if err != nil {
		log.Fatal(err)
	}
	evaluationHandler := handler.NewEvaluationHandler(evaluationManager)
	evaluationController := controller.NewEvaluationController(evaluationHandler)
	evaluationController.Inject(apiV1)

	stateHandler := handler.NewStateHandler(&db.MongoDBStateRepo{})
	stateController := controller.NewStateController(stateHandler)
	stateController.Inject(apiV1)

	sequenceStateMaterializedView := sequencehooks.NewSequenceStateMaterializedView(&db.MongoDBStateRepo{})
	shipyardController.AddSequenceTriggeredHook(sequenceStateMaterializedView)
	shipyardController.AddSequenceTaskTriggeredHook(sequenceStateMaterializedView)
	shipyardController.AddSequenceTaskStartedHook(sequenceStateMaterializedView)
	shipyardController.AddSequenceTaskFinishedHook(sequenceStateMaterializedView)
	shipyardController.AddSubSequenceFinishedHook(sequenceStateMaterializedView)
	shipyardController.AddSequenceFinishedHook(sequenceStateMaterializedView)

	engine.Static("/swagger-ui", "./swagger-ui")
	engine.Run()
}

func createMaterializedView() *db.ProjectsMaterializedView {
	projectesMaterializedView := &db.ProjectsMaterializedView{
		ProjectRepo:     createProjectRepo(),
		EventsRetriever: createEventsRepo(),
	}
	return projectesMaterializedView
}

func createProjectRepo() *db.MongoDBProjectsRepo {
	return &db.MongoDBProjectsRepo{}
}

func createEventsRepo() *db.MongoDBEventsRepo {
	return &db.MongoDBEventsRepo{}
}

func createEventQueueRepo() *db.MongoDBEventQueueRepo {
	return &db.MongoDBEventQueueRepo{}
}

func createTaskSequenceRepo() *db.TaskSequenceMongoDBRepo {
	return &db.TaskSequenceMongoDBRepo{}
}

func createSecretStore(kubeAPI *kubernetes.Clientset) *common.K8sSecretStore {
	return common.NewK8sSecretStore(kubeAPI)
}

// GetKubeAPI godoc
func createKubeAPI() (*kubernetes.Clientset, error) {
	var config *rest.Config
	config, err := rest.InClusterConfig()

	if err != nil {
		return nil, err
	}

	kubeAPI, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return kubeAPI, nil
}
