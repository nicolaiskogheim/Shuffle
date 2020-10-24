package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloud.google.com/go/datastore"
	"google.golang.org/api/option"

	"google.golang.org/grpc"
)

type endpoint struct {
	handler http.HandlerFunc
	path    string
	method  string
}

// TestTestAuthenticationRequired tests that the handlers in the `handlers`
// variable returns 401 Unauthorized when called without credentials.
func TestAuthenticationRequired(t *testing.T) {
	handlers := []endpoint{
		{handler: handleSendalert, path: "/functions/sendmail", method: "POST"},
		{handler: handleNewOutlookRegister, path: "/functions/outlook/register", method: "GET"},
		{handler: handleGetOutlookFolders, path: "/functions/outlook/getFolders", method: "GET"},
		{handler: handleApiGeneration, path: "/api/v1/users/generateapikey", method: "GET"},
		// handleLogin generates nil pointer exception
		{handler: handleLogin, path: "/api/v1/users/login", method: "POST"}, // prob not this one
		// handleRegister generates nil pointer exception
		{handler: handleRegister, path: "/api/v1/users/register", method: "POST"},
		{handler: handleGetUsers, path: "/api/v1/users/getusers", method: "GET"},
		{handler: handleInfo, path: "/api/v1/users/getinfo", method: "GET"},
		{handler: handleSettings, path: "/api/v1/users/getsettings", method: "GET"},
		{handler: handleUpdateUser, path: "/api/v1/users/updateuser", method: "PUT"},
		{handler: deleteUser, path: "/api/v1/users/123", method: "DELETE"},
		// handlePasswordChange generates nil pointer exception
		{handler: handlePasswordChange, path: "/api/v1/users/passwordchange", method: "POST"},
		{handler: handleGetUsers, path: "/api/v1/users", method: "GET"},
		{handler: handleGetEnvironments, path: "/api/v1/getenvironments", method: "GET"},
		{handler: handleSetEnvironments, path: "/api/v1/setenvironments", method: "PUT"},

		// handleWorkflowQueue generates nil pointer exception
		{handler: handleWorkflowQueue, path: "/api/v1/streams", method: "POST"},
		// handleGetStreamResults generates nil pointer exception
		{handler: handleGetStreamResults, path: "/api/v1/streams/results", method: "POST"},

		{handler: handleAppHotloadRequest, path: "/api/v1/apps/run_hotload", method: "GET"},
		{handler: loadSpecificApps, path: "/api/v1/apps/get_existing", method: "POST"},
		{handler: updateWorkflowAppConfig, path: "/api/v1/apps/123", method: "PATCH"},
		{handler: validateAppInput, path: "/api/v1/apps/validate", method: "POST"},
		{handler: deleteWorkflowApp, path: "/api/v1/apps/123", method: "DELETE"},
		{handler: getWorkflowAppConfig, path: "/api/v1/apps/123/config", method: "GET"},
		{handler: getWorkflowApps, path: "/api/v1/apps", method: "GET"},
		{handler: setNewWorkflowApp, path: "/api/v1/apps", method: "PUT"},
		{handler: getSpecificApps, path: "/api/v1/apps/search", method: "POST"},

		{handler: getAppAuthentication, path: "/api/v1/apps/authentication", method: "GET"},
		{handler: addAppAuthentication, path: "/api/v1/apps/authentication", method: "PUT"},
		{handler: deleteAppAuthentication, path: "/api/v1/apps/authentication/123", method: "DELETE"},

		{handler: validateAppInput, path: "/api/v1/workflows/apps/validate", method: "POST"},
		{handler: getWorkflowApps, path: "/api/v1/workflows/apps", method: "GET"},
		{handler: setNewWorkflowApp, path: "/api/v1/workflows/apps", method: "PUT"},

		{handler: getWorkflows, path: "/api/v1/workflows", method: "GET"},
		{handler: setNewWorkflow, path: "/api/v1/workflows", method: "POST"},
		{handler: handleGetWorkflowqueue, path: "/api/v1/workflows/queue", method: "GET"},
		{handler: handleGetWorkflowqueueConfirm, path: "/api/v1/workflows/queue/confirm", method: "POST"},
		{handler: handleGetSchedules, path: "/api/v1/workflows/schedules", method: "GET"},
		{handler: loadSpecificWorkflows, path: "/api/v1/workflows/download_remote", method: "POST"},
		{handler: executeWorkflow, path: "/api/v1/workflows/123/execute", method: "GET"},
		{handler: scheduleWorkflow, path: "/api/v1/workflows/123/schedule", method: "POST"},
		{handler: stopSchedule, path: "/api/v1/workflows/123/schedule/abc", method: "DELETE"},
		// createOutlookSub generates nil pointer exception
		{handler: createOutlookSub, path: "/api/v1/workflows/123/outlook", method: "POST"},
		// handleDeleteOutlookSub generates nil pointer exception
		{handler: handleDeleteOutlookSub, path: "/api/v1/workflows/123/outlook/abc", method: "DELETE"},
		{handler: getWorkflowExecutions, path: "/api/v1/workflows/123/executions", method: "GET"},
		{handler: abortExecution, path: "/api/v1/workflows/123/executions/abc/abort", method: "GET"},
		{handler: getSpecificWorkflow, path: "/api/v1/workflows/123", method: "GET"},
		{handler: saveWorkflow, path: "/api/v1/workflows/123", method: "PUT"},
		{handler: deleteWorkflow, path: "/api/v1/workflows/123", method: "DELETE"},

		{handler: handleNewHook, path: "/api/v1/hooks/new", method: "POST"},
		{handler: handleWebhookCallback, path: "/api/v1/hooks/123", method: "POST"},
		{handler: handleDeleteHook, path: "/api/v1/hooks/123/delete", method: "DELETE"},

		{handler: handleGetSpecificTrigger, path: "/api/v1/triggers/123", method: "GET"},
		{handler: handleGetSpecificStats, path: "/api/v1/stats/123", method: "GET"},

		{handler: verifySwagger, path: "/api/v1/verify_swagger", method: "POST"},
		{handler: verifySwagger, path: "/api/v1/verify_openapi", method: "POST"},
		{handler: echoOpenapiData, path: "/api/v1/get_openapi_uri", method: "POST"},
		{handler: echoOpenapiData, path: "/api/v1/validate_openapi", method: "POST"},
		{handler: validateSwagger, path: "/api/v1/validate_openapi", method: "POST"},
		{handler: getOpenapi, path: "/api/v1/get_openapi", method: "GET"},

		{handler: cleanupExecutions, path: "/api/v1/execution_cleanup", method: "GET"},
	}

	var err error
	ctx := context.Background()

	// Most handlers requires database access in order to not crash or cause
	// nil pointer issues.
	// To start a local database instance, run:
	//   docker-compose up database
	// To let the tests know about the database, run:
	//   DATASTORE_EMULATOR_HOST=0.0.0.0:8000 go test
	dbclient, err = datastore.NewClient(ctx, gceProject, option.WithGRPCDialOption(grpc.WithNoProxy()))
	if err != nil {
		t.Fatal(err)
	}

	dummyBody := bytes.NewBufferString("dummy")

	for _, e := range handlers {
		req, err := http.NewRequest(e.method, e.path, dummyBody)
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(e.handler)

		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusUnauthorized {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusUnauthorized)
		}
	}
}

func TestAuthenticationNotRequired(t *testing.T) {
	handlers := []endpoint{
		// checkAdminLogin returns 200 OK when not logged in
		{handler: checkAdminLogin, path: "/api/v1/users/checkusers", method: "GET"},
		// handleLogout returns 200 OK when not logged in
		{handler: handleLogout, path: "/api/v1/users/logout", method: "POST"},
		// getDocsList returns 200 OK when not logged in
		{handler: getDocList, path: "/api/v1/docs", method: "GET"},
		// getDocs returns 200 OK when not logged in
		{handler: getDocs, path: "/api/v1/docs/123", method: "GET"},
	}

	for _, e := range handlers {
		req, err := http.NewRequest(e.method, e.path, nil)
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(e.handler)

		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
	}
}
