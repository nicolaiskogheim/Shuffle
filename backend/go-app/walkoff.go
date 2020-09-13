package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
	scheduler "cloud.google.com/go/scheduler/apiv1"
	gyaml "github.com/ghodss/yaml"
	"github.com/h2non/filetype"
	uuid "github.com/satori/go.uuid"
	"google.golang.org/api/cloudfunctions/v1"
	schedulerpb "google.golang.org/genproto/googleapis/cloud/scheduler/v1"

	newscheduler "github.com/carlescere/scheduler"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	http2 "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	//"github.com/gorilla/websocket"
	//"google.golang.org/appengine"
	//"google.golang.org/appengine/memcache"
	//"cloud.google.com/go/firestore"
	// "google.golang.org/api/option"
)

var localBase = "http://localhost:5001"
var baseEnvironment = "onprem"

var cloudname = "cloud"
var defaultLocation = "europe-west2"
var scheduledJobs = map[string]*newscheduler.Job{}

// To test out firestore before potential merge
//var upgrader = websocket.Upgrader{
//	ReadBufferSize:  1024,
//	WriteBufferSize: 1024,
//	CheckOrigin: func(r *http.Request) bool {
//		return true
//	},
//}

type ExecutionRequest struct {
	ExecutionId       string   `json:"execution_id"`
	ExecutionArgument string   `json:"execution_argument"`
	ExecutionSource   string   `json:"execution_source"`
	WorkflowId        string   `json:"workflow_id"`
	Environments      []string `json:"environments"`
	Authorization     string   `json:"authorization"`
	Status            string   `json:"status"`
	Start             string   `json:"start"`
	Type              string   `json:"type"`
}

type Org struct {
	Name  string `json:"name"`
	Org   string `json:"org"`
	Users []User `json:"users"`
	Id    string `json:"id"`
}

type AppAuthenticationStorage struct {
	Active        bool                  `json:"active" datastore:"active"`
	Label         string                `json:"label" datastore:"label"`
	Id            string                `json:"id" datastore:"id"`
	App           WorkflowApp           `json:"app" datastore:"app,noindex"`
	Fields        []AuthenticationStore `json:"fields" datastore:"fields"`
	Usage         []AuthenticationUsage `json:"usage" datastore:"usage"`
	WorkflowCount int64                 `json:"workflow_count" datastore:"workflow_count"`
	NodeCount     int64                 `json:"node_count" datastore:"node_count"`
}

type AuthenticationUsage struct {
	WorkflowId string   `json:"workflow_id" datastore:"workflow_id"`
	Nodes      []string `json:"nodes" datastore:"nodes"`
}

// An app inside Shuffle
// Source      string `json:"source" datastore:"soure" yaml:"source"` - downloadlocation
type WorkflowApp struct {
	Name          string `json:"name" yaml:"name" required:true datastore:"name"`
	IsValid       bool   `json:"is_valid" yaml:"is_valid" required:true datastore:"is_valid"`
	ID            string `json:"id" yaml:"id,omitempty" required:false datastore:"id"`
	Link          string `json:"link" yaml:"link" required:false datastore:"link,noindex"`
	AppVersion    string `json:"app_version" yaml:"app_version" required:true datastore:"app_version"`
	SharingConfig string `json:"sharing_config" yaml:"sharing_config" datastore:"sharing_config"`
	Generated     bool   `json:"generated" yaml:"generated" required:false datastore:"generated"`
	Downloaded    bool   `json:"downloaded" yaml:"downloaded" required:false datastore:"downloaded"`
	Sharing       bool   `json:"sharing" yaml:"sharing" required:false datastore:"sharing"`
	Verified      bool   `json:"verified" yaml:"verified" required:false datastore:"verified"`
	Activated     bool   `json:"activated" yaml:"activated" required:false datastore:"activated"`
	Tested        bool   `json:"tested" yaml:"tested" required:false datastore:"tested"`
	Owner         string `json:"owner" datastore:"owner" yaml:"owner"`
	Hash          string `json:"hash" datastore:"hash" yaml:"hash"` // api.yaml+dockerfile+src/app.py for apps
	PrivateID     string `json:"private_id" yaml:"private_id" required:false datastore:"private_id"`
	Description   string `json:"description" datastore:"description,noindex" required:false yaml:"description"`
	Environment   string `json:"environment" datastore:"environment" required:true yaml:"environment"`
	SmallImage    string `json:"small_image" datastore:"small_image,noindex" required:false yaml:"small_image"`
	LargeImage    string `json:"large_image" datastore:"large_image,noindex" yaml:"large_image" required:false`
	ContactInfo   struct {
		Name string `json:"name" datastore:"name" yaml:"name"`
		Url  string `json:"url" datastore:"url" yaml:"url"`
	} `json:"contact_info" datastore:"contact_info" yaml:"contact_info" required:false`
	Actions        []WorkflowAppAction `json:"actions" yaml:"actions" required:true datastore:"actions,noindex"`
	Authentication Authentication      `json:"authentication" yaml:"authentication" required:false datastore:"authentication"`
	Tags           []string            `json:"tags" yaml:"tags" required:false datastore:"activated"`
	Categories     []string            `json:"categories" yaml:"categories" required:false datastore:"categories"`
}

type WorkflowAppActionParameter struct {
	Description    string           `json:"description" datastore:"description,noindex" yaml:"description"`
	ID             string           `json:"id" datastore:"id" yaml:"id,omitempty"`
	Name           string           `json:"name" datastore:"name" yaml:"name"`
	Example        string           `json:"example" datastore:"example" yaml:"example"`
	Value          string           `json:"value" datastore:"value" yaml:"value,omitempty"`
	Multiline      bool             `json:"multiline" datastore:"multiline" yaml:"multiline"`
	Options        []string         `json:"options" datastore:"options" yaml:"options"`
	ActionField    string           `json:"action_field" datastore:"action_field" yaml:"actionfield,omitempty"`
	Variant        string           `json:"variant" datastore:"variant" yaml:"variant,omitempty"`
	Required       bool             `json:"required" datastore:"required" yaml:"required"`
	Configuration  bool             `json:"configuration" datastore:"configuration" yaml:"configuration"`
	Tags           []string         `json:"tags" datastore:"tags" yaml:"tags"`
	Schema         SchemaDefinition `json:"schema" datastore:"schema" yaml:"schema"`
	SkipMulticheck bool             `json:"skip_multicheck" datastore:"skip_multicheck" yaml:"skip_multicheck"`
}

type SchemaDefinition struct {
	Type string `json:"type" datastore:"type"`
}

type WorkflowAppAction struct {
	Description       string                       `json:"description" datastore:"description,noindex"`
	ID                string                       `json:"id" datastore:"id" yaml:"id,omitempty"`
	Name              string                       `json:"name" datastore:"name"`
	Label             string                       `json:"label" datastore:"label"`
	NodeType          string                       `json:"node_type" datastore:"node_type"`
	Environment       string                       `json:"environment" datastore:"environment"`
	Sharing           bool                         `json:"sharing" datastore:"sharing"`
	PrivateID         string                       `json:"private_id" datastore:"private_id"`
	AppID             string                       `json:"app_id" datastore:"app_id"`
	Tags              []string                     `json:"tags" datastore:"tags" yaml:"tags"`
	Authentication    []AuthenticationStore        `json:"authentication" datastore:"authentication,noindex" yaml:"authentication,omitempty"`
	Tested            bool                         `json:"tested" datastore:"tested" yaml:"tested"`
	Parameters        []WorkflowAppActionParameter `json:"parameters" datastore: "parameters"`
	ExecutionVariable struct {
		Description string `json:"description" datastore:"description,noindex"`
		ID          string `json:"id" datastore:"id"`
		Name        string `json:"name" datastore:"name"`
		Value       string `json:"value" datastore:"value"`
	} `json:"execution_variable" datastore:"execution_variables"`
	Returns struct {
		Description string           `json:"description" datastore:"returns" yaml:"description,omitempty"`
		Example     string           `json:"example" datastore:"example" yaml:"example"`
		ID          string           `json:"id" datastore:"id" yaml:"id,omitempty"`
		Schema      SchemaDefinition `json:"schema" datastore:"schema" yaml:"schema"`
	} `json:"returns" datastore:"returns"`
	AuthenticationId string `json:"authentication_id" datastore:"authentication_id"`
	Example          string `json:"example" datastore:"example" yaml:"example"`
	AuthNotRequired  bool   `json:"auth_not_required" datastore:"auth_not_required" yaml:"auth_not_required"`
}

// FIXME: Generate a callback authentication ID?
type WorkflowExecution struct {
	Type               string         `json:"type" datastore:"type"`
	Status             string         `json:"status" datastore:"status"`
	Start              string         `json:"start" datastore:"start"`
	ExecutionArgument  string         `json:"execution_argument" datastore:"execution_argument,noindex"`
	ExecutionId        string         `json:"execution_id" datastore:"execution_id"`
	ExecutionSource    string         `json:"execution_source" datastore:"execution_source"`
	WorkflowId         string         `json:"workflow_id" datastore:"workflow_id"`
	LastNode           string         `json:"last_node" datastore:"last_node"`
	Authorization      string         `json:"authorization" datastore:"authorization"`
	Result             string         `json:"result" datastore:"result,noindex"`
	StartedAt          int64          `json:"started_at" datastore:"started_at"`
	CompletedAt        int64          `json:"completed_at" datastore:"completed_at"`
	ProjectId          string         `json:"project_id" datastore:"project_id"`
	Locations          []string       `json:"locations" datastore:"locations"`
	Workflow           Workflow       `json:"workflow" datastore:"workflow,noindex"`
	Results            []ActionResult `json:"results" datastore:"results,noindex"`
	ExecutionVariables []struct {
		Description string `json:"description" datastore:"description,noindex"`
		ID          string `json:"id" datastore:"id"`
		Name        string `json:"name" datastore:"name"`
		Value       string `json:"value" datastore:"value,noindex"`
	} `json:"execution_variables,omitempty" datastore:"execution_variables,omitempty"`
}

// This is for the nodes in a workflow, NOT the app action itself.
type Action struct {
	AppName           string                       `json:"app_name" datastore:"app_name"`
	AppVersion        string                       `json:"app_version" datastore:"app_version"`
	AppID             string                       `json:"app_id" datastore:"app_id"`
	Errors            []string                     `json:"errors" datastore:"errors"`
	ID                string                       `json:"id" datastore:"id"`
	IsValid           bool                         `json:"is_valid" datastore:"is_valid"`
	IsStartNode       bool                         `json:"isStartNode" datastore:"isStartNode"`
	Sharing           bool                         `json:"sharing" datastore:"sharing"`
	PrivateID         string                       `json:"private_id" datastore:"private_id"`
	Label             string                       `json:"label" datastore:"label"`
	SmallImage        string                       `json:"small_image" datastore:"small_image,noindex" required:false yaml:"small_image"`
	LargeImage        string                       `json:"large_image" datastore:"large_image,noindex" yaml:"large_image" required:false`
	Environment       string                       `json:"environment" datastore:"environment"`
	Name              string                       `json:"name" datastore:"name"`
	Parameters        []WorkflowAppActionParameter `json:"parameters" datastore: "parameters,noindex"`
	ExecutionVariable struct {
		Description string `json:"description" datastore:"description,noindex"`
		ID          string `json:"id" datastore:"id"`
		Name        string `json:"name" datastore:"name"`
		Value       string `json:"value" datastore:"value,noindex"`
	} `json:"execution_variable,omitempty" datastore:"execution_variable,omitempty"`
	Position struct {
		X float64 `json:"x" datastore:"x"`
		Y float64 `json:"y" datastore:"y"`
	} `json:"position"`
	Priority         int    `json:"priority" datastore:"priority"`
	AuthenticationId string `json:"authentication_id" datastore:"authentication_id"`
	Example          string `json:"example" datastore:"example"`
	AuthNotRequired  bool   `json:"auth_not_required" datastore:"auth_not_required" yaml:"auth_not_required"`
}

// Added environment for location to execute
type Trigger struct {
	AppName         string                       `json:"app_name" datastore:"app_name"`
	Description     string                       `json:"description" datastore:"description,noindex"`
	LongDescription string                       `json:"long_description" datastore:"long_description"`
	Status          string                       `json:"status" datastore:"status"`
	AppVersion      string                       `json:"app_version" datastore:"app_version"`
	Errors          []string                     `json:"errors" datastore:"errors"`
	ID              string                       `json:"id" datastore:"id"`
	IsValid         bool                         `json:"is_valid" datastore:"is_valid"`
	IsStartNode     bool                         `json:"isStartNode" datastore:"isStartNode"`
	Label           string                       `json:"label" datastore:"label"`
	SmallImage      string                       `json:"small_image" datastore:"small_image,noindex" required:false yaml:"small_image"`
	LargeImage      string                       `json:"large_image" datastore:"large_image,noindex" yaml:"large_image" required:false`
	Environment     string                       `json:"environment" datastore:"environment"`
	TriggerType     string                       `json:"trigger_type" datastore:"trigger_type"`
	Name            string                       `json:"name" datastore:"name"`
	Tags            []string                     `json:"tags" datastore:"tags" yaml:"tags"`
	Parameters      []WorkflowAppActionParameter `json:"parameters" datastore: "parameters,noindex"`
	Position        struct {
		X float64 `json:"x" datastore:"x"`
		Y float64 `json:"y" datastore:"y"`
	} `json:"position"`
	Priority int `json:"priority" datastore:"priority"`
}

type Branch struct {
	DestinationID string      `json:"destination_id" datastore:"destination_id"`
	ID            string      `json:"id" datastore:"id"`
	SourceID      string      `json:"source_id" datastore:"source_id"`
	Label         string      `json:"label" datastore:"label"`
	HasError      bool        `json:"has_errors" datastore: "has_errors"`
	Conditions    []Condition `json:"conditions" datastore: "conditions,noindex"`
}

// Same format for a lot of stuff
type Condition struct {
	Condition   WorkflowAppActionParameter `json:"condition" datastore:"condition"`
	Source      WorkflowAppActionParameter `json:"source" datastore:"source"`
	Destination WorkflowAppActionParameter `json:"destination" datastore:"destination"`
}

type Schedule struct {
	Name              string `json:"name" datastore:"name"`
	Frequency         string `json:"frequency" datastore:"frequency"`
	ExecutionArgument string `json:"execution_argument" datastore:"execution_argument,noindex"`
	Id                string `json:"id" datastore:"id"`
}

type Workflow struct {
	Actions       []Action   `json:"actions" datastore:"actions,noindex"`
	Branches      []Branch   `json:"branches" datastore:"branches,noindex"`
	Triggers      []Trigger  `json:"triggers" datastore:"triggers,noindex"`
	Schedules     []Schedule `json:"schedules" datastore:"schedules,noindex"`
	Configuration struct {
		ExitOnError  bool `json:"exit_on_error" datastore:"exit_on_error"`
		StartFromTop bool `json:"start_from_top" datastore:"start_from_top"`
	} `json:"configuration,omitempty" datastore:"configuration"`
	Errors            []string `json:"errors,omitempty" datastore:"errors"`
	Tags              []string `json:"tags,omitempty" datastore:"tags"`
	ID                string   `json:"id" datastore:"id"`
	IsValid           bool     `json:"is_valid" datastore:"is_valid"`
	Name              string   `json:"name" datastore:"name"`
	Description       string   `json:"description" datastore:"description,noindex"`
	Start             string   `json:"start" datastore:"start"`
	Owner             string   `json:"owner" datastore:"owner"`
	Sharing           string   `json:"sharing" datastore:"sharing"`
	Org               []Org    `json:"org,omitempty" datastore:"org"`
	ExecutingOrg      Org      `json:"execution_org,omitempty" datastore:"execution_org"`
	WorkflowVariables []struct {
		Description string `json:"description" datastore:"description,noindex"`
		ID          string `json:"id" datastore:"id"`
		Name        string `json:"name" datastore:"name"`
		Value       string `json:"value" datastore:"value"`
	} `json:"workflow_variables" datastore:"workflow_variables"`
	ExecutionVariables []struct {
		Description string `json:"description" datastore:"description,noindex"`
		ID          string `json:"id" datastore:"id"`
		Name        string `json:"name" datastore:"name"`
		Value       string `json:"value" datastore:"value,noindex"`
	} `json:"execution_variables,omitempty" datastore:"execution_variables"`
}

type ActionResult struct {
	Action        Action `json:"action" datastore:"action,noindex"`
	ExecutionId   string `json:"execution_id" datastore:"execution_id"`
	Authorization string `json:"authorization" datastore:"authorization"`
	Result        string `json:"result" datastore:"result,noindex"`
	StartedAt     int64  `json:"started_at" datastore:"started_at"`
	CompletedAt   int64  `json:"completed_at" datastore:"completed_at"`
	Status        string `json:"status" datastore:"status"`
}

type Authentication struct {
	Required   bool                   `json:"required" datastore:"required" yaml:"required" `
	Parameters []AuthenticationParams `json:"parameters" datastore:"parameters" yaml:"parameters"`
}

type AuthenticationParams struct {
	Description string           `json:"description" datastore:"description,noindex" yaml:"description"`
	ID          string           `json:"id" datastore:"id" yaml:"id"`
	Name        string           `json:"name" datastore:"name" yaml:"name"`
	Example     string           `json:"example" datastore:"example" yaml:"example"`
	Value       string           `json:"value,omitempty" datastore:"value" yaml:"value"`
	Multiline   bool             `json:"multiline" datastore:"multiline" yaml:"multiline"`
	Required    bool             `json:"required" datastore:"required" yaml:"required"`
	In          string           `json:"in" datastore:"in" yaml:"in"`
	Schema      SchemaDefinition `json:"schema" datastore:"schema" yaml:"schema"`
	Scheme      string           `json:"scheme" datastore:"scheme" yaml:"scheme"` // Deprecated
}

type AuthenticationStore struct {
	Key   string `json:"key" datastore:"key"`
	Value string `json:"value" datastore:"value"`
}

type ExecutionRequestWrapper struct {
	Data []ExecutionRequest `json:"data"`
}

// This might be... a bit off, but that's fine :)
// This might also be stupid, as we want timelines and such
// Anyway, these are super basic stupid stats.
func increaseStatisticsField(ctx context.Context, fieldname, id string, amount int64) error {

	// 1. Get current stats
	// 2. Increase field(s)
	// 3. Put new stats
	statisticsId := "global_statistics"
	nameKey := fieldname
	key := datastore.NameKey(statisticsId, nameKey, nil)

	statisticsItem := StatisticsItem{}
	newData := StatisticsData{
		Timestamp: int64(time.Now().Unix()),
		Amount:    amount,
		Id:        id,
	}

	if err := dbclient.Get(ctx, key, &statisticsItem); err != nil {
		// Should init
		if strings.Contains(fmt.Sprintf("%s", err), "entity") {
			statisticsItem = StatisticsItem{
				Total:     amount,
				Fieldname: fieldname,
				Data: []StatisticsData{
					newData,
				},
			}

			if _, err := dbclient.Put(ctx, key, &statisticsItem); err != nil {
				log.Printf("Error setting base stats: %s", err)
				return err
			}

			return nil
		}
		//log.Printf("STATSERR: %s", err)

		return err
	}

	statisticsItem.Total += amount
	statisticsItem.Data = append(statisticsItem.Data, newData)

	// New struct, to not add body, author etc
	if _, err := dbclient.Put(ctx, key, &statisticsItem); err != nil {
		log.Printf("Error stats to %s: %s", fieldname, err)
		return err
	}

	//log.Printf("Stats: %#v", statisticsItem)

	return nil
}

func setWorkflowQueue(ctx context.Context, executionRequests ExecutionRequestWrapper, id string) error {
	key := datastore.NameKey("workflowqueue", id, nil)

	// New struct, to not add body, author etc
	if _, err := dbclient.Put(ctx, key, &executionRequests); err != nil {
		log.Printf("Error adding workflow queue: %s", err)
		return err
	}

	return nil
}

func getWorkflowQueue(ctx context.Context, id string) (ExecutionRequestWrapper, error) {
	key := datastore.NameKey("workflowqueue", id, nil)
	workflows := ExecutionRequestWrapper{}
	if err := dbclient.Get(ctx, key, &workflows); err != nil {
		return ExecutionRequestWrapper{}, err
	}

	return workflows, nil
}

//func setWorkflowqueuetest(id string) {
//	data := ExecutionRequestWrapper{
//		Data: []ExecutionRequest{
//			ExecutionRequest{
//				ExecutionId:   "2349bf96-51ad-68d2-5ca6-75ef8f7ee814",
//				WorkflowId:    "8e344a2e-db51-448f-804c-eb959a32c139",
//				Authorization: "wut",
//			},
//		},
//	}
//
//	err := setWorkflowQueue(data, id)
//	if err != nil {
//		log.Printf("Fail: %s", err)
//	}
//}

// Frequency = cronjob OR minutes between execution
func createSchedule(ctx context.Context, scheduleId, workflowId, name, startNode, frequency string, body []byte) error {
	var err error
	testSplit := strings.Split(frequency, "*")
	cronJob := ""
	newfrequency := 0

	if len(testSplit) > 5 {
		cronJob = frequency
	} else {
		newfrequency, err = strconv.Atoi(frequency)
		if err != nil {
			log.Printf("Failed to parse time: %s", err)
			return err
		}

		//if int(newfrequency) < 60 {
		//	cronJob = fmt.Sprintf("*/%s * * * *")
		//} else if int(newfrequency) <
	}

	// Reverse. Can't handle CRON, only numbers
	if len(cronJob) > 0 {
		return errors.New("cronJob isn't formatted correctly")
	}

	if newfrequency < 1 {
		return errors.New("Frequency has to be more than 0")
	}

	//log.Printf("CRON: %s, body: %s", cronJob, string(body))

	// FIXME:
	// This may run multiple places if multiple servers,
	// but that's a future problem
	//log.Printf("BODY: %s", string(body))
	parsedArgument := strings.Replace(string(body), "\"", "\\\"", -1)
	bodyWrapper := fmt.Sprintf(`{"start": "%s", "execution_source": "schedule", "execution_argument": "%s"}`, startNode, parsedArgument)
	log.Printf("WRAPPER BODY: \n%s", bodyWrapper)
	job := func() {
		request := &http.Request{
			Method: "POST",
			Body:   ioutil.NopCloser(strings.NewReader(bodyWrapper)),
		}

		_, _, err := handleExecution(workflowId, Workflow{}, request)
		if err != nil {
			log.Printf("Failed to execute %s: %s", workflowId, err)
		}
	}

	log.Printf("Starting frequency: %d", newfrequency)
	jobret, err := newscheduler.Every(newfrequency).Seconds().NotImmediately().Run(job)
	if err != nil {
		log.Printf("Failed to schedule workflow: %s", err)
		return err
	}

	//scheduledJobs = append(scheduledJobs, jobret)
	scheduledJobs[scheduleId] = jobret

	// Doesn't need running/not running. If stopped, we just delete it.
	timeNow := int64(time.Now().Unix())
	schedule := ScheduleOld{
		Id:                   scheduleId,
		WorkflowId:           workflowId,
		StartNode:            startNode,
		Argument:             string(body),
		WrappedArgument:      bodyWrapper,
		Seconds:              newfrequency,
		CreationTime:         timeNow,
		LastModificationtime: timeNow,
		LastRuntime:          timeNow,
	}

	err = setSchedule(ctx, schedule)
	if err != nil {
		log.Printf("Failed to set schedule: %s", err)
		return err
	}

	// FIXME - Create a real schedule based on cron:
	// 1. Parse the cron in a function to match this schedule
	// 2. Make main init check for schedules that aren't running

	return nil
}

func handleGetWorkflowqueueConfirm(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// FIXME: Add authentication?
	id := request.Header.Get("Org-Id")
	if len(id) == 0 {
		log.Printf("No Org-Id header set - confirm")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Specify the org-id header."}`)))
		return
	}

	//setWorkflowqueuetest(id)
	ctx := context.Background()
	executionRequests, err := getWorkflowQueue(ctx, id)
	if err != nil {
		log.Printf("(1) Failed reading body for workflowqueue: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Entity parsing error - confirm"}`)))
		return
	}

	if len(executionRequests.Data) == 0 {
		log.Printf("No requests to fix. Why did this request occur?")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Some error"}`)))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Println("Failed reading body for stream result queue")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	// Getting from the request
	//log.Println(string(body))
	var removeExecutionRequests ExecutionRequestWrapper
	err = json.Unmarshal(body, &removeExecutionRequests)
	if err != nil {
		log.Printf("Failed executionrequest in queue unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	if len(removeExecutionRequests.Data) == 0 {
		log.Printf("No requests to fix remove")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Some removal error"}`)))
		return
	}

	// remove items from DB
	var newExecutionRequests ExecutionRequestWrapper
	for _, execution := range executionRequests.Data {
		found := false
		for _, removeExecution := range removeExecutionRequests.Data {
			if removeExecution.ExecutionId == execution.ExecutionId && removeExecution.WorkflowId == execution.WorkflowId {
				found = true
				break
			}
		}

		if !found {
			newExecutionRequests.Data = append(newExecutionRequests.Data, execution)
		}
	}

	// Push only the remaining to the DB (remove)
	if len(executionRequests.Data) != len(newExecutionRequests.Data) {
		err := setWorkflowQueue(ctx, newExecutionRequests, id)
		if err != nil {
			log.Printf("Fail: %s", err)
		}
	}

	//newjson, err := json.Marshal(removeExecutionRequests)
	//if err != nil {
	//	resp.WriteHeader(401)
	//	resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow execution"}`)))
	//	return
	//}

	resp.WriteHeader(200)
	resp.Write([]byte("OK"))
}

// FIXME: Authenticate this one? Can org ID be auth enough?
// (especially since we have a default: shuffle)
func handleGetWorkflowqueue(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	id := request.Header.Get("Org-Id")
	if len(id) == 0 {
		log.Printf("No org-id header set")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Specify the org-id header."}`)))
		return
	}

	ctx := context.Background()
	executionRequests, err := getWorkflowQueue(ctx, id)
	if err != nil {
		// Skipping as this comes up over and over
		//log.Printf("(2) Failed reading body for workflowqueue: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	if len(executionRequests.Data) == 0 {
		executionRequests.Data = []ExecutionRequest{}
	} else {
		log.Printf("[INFO] Executionrequests: %d", len(executionRequests.Data))
	}

	newjson, err := json.Marshal(executionRequests)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow execution"}`)))
		return
	}

	resp.WriteHeader(200)
	resp.Write(newjson)
}

func handleGetStreamResults(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Println("Failed reading body for stream result queue")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	var actionResult ActionResult
	err = json.Unmarshal(body, &actionResult)
	if err != nil {
		log.Printf("Failed ActionResult unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	ctx := context.Background()
	workflowExecution, err := getWorkflowExecution(ctx, actionResult.ExecutionId)
	if err != nil {
		log.Printf("Failed getting execution (streamresult) %s: %s", actionResult.ExecutionId, err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Bad authorization key or execution_id might not exist."}`)))
		return
	}

	// Authorization is done here
	if workflowExecution.Authorization != actionResult.Authorization {
		log.Printf("Bad authorization key when getting stream results %s.", actionResult.ExecutionId)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Bad authorization key or execution_id might not exist."}`)))
		return
	}

	newjson, err := json.Marshal(workflowExecution)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow execution"}`)))
		return
	}

	resp.WriteHeader(200)
	resp.Write(newjson)

}

// Finds the child nodes of a node in execution and returns them
// Used if e.g. a node in a branch is exited, and all children have to be stopped
func findChildNodes(workflowExecution WorkflowExecution, nodeId string) []string {
	//log.Printf("\nNODE TO FIX: %s\n\n", nodeId)
	allChildren := []string{nodeId}

	// 1. Find children of this specific node
	// 2. Find the children of those nodes etc.
	for _, branch := range workflowExecution.Workflow.Branches {
		if branch.SourceID == nodeId {
			//log.Printf("Children: %s", branch.DestinationID)
			allChildren = append(allChildren, branch.DestinationID)

			childNodes := findChildNodes(workflowExecution, branch.DestinationID)
			for _, bottomChild := range childNodes {
				found := false
				for _, topChild := range allChildren {
					if topChild == bottomChild {
						found = true
						break
					}
				}

				if !found {
					allChildren = append(allChildren, bottomChild)
				}
			}
		}
	}

	// Remove potential duplicates
	newNodes := []string{}
	for _, tmpnode := range allChildren {
		found := false
		for _, newnode := range newNodes {
			if newnode == tmpnode {
				found = true
				break
			}
		}

		if !found {
			newNodes = append(newNodes, tmpnode)
		}
	}

	return newNodes
}

func handleWorkflowQueue(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Println("(3) Failed reading body for workflowqueue")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	var actionResult ActionResult
	err = json.Unmarshal(body, &actionResult)
	if err != nil {
		log.Printf("Failed ActionResult unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	// 1. Get the WorkflowExecution(ExecutionId) from the database
	// 2. if ActionResult.Authentication != WorkflowExecution.Authentication -> exit
	// 3. Add to and update actionResult in workflowExecution
	// 4. Push to db
	// IF FAIL: Set executionstatus: abort or cancel

	ctx := context.Background()
	workflowExecution, err := getWorkflowExecution(ctx, actionResult.ExecutionId)
	if err != nil {
		log.Printf("Failed getting execution (workflowqueue) %s: %s", actionResult.ExecutionId, err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed getting execution ID %s because it doesn't exist."}`, actionResult.ExecutionId)))
		return
	}

	if workflowExecution.Authorization != actionResult.Authorization {
		log.Printf("Bad authorization key when updating node (workflowQueue) %s. Want: %s, Have: %s", actionResult.ExecutionId, workflowExecution.Authorization, actionResult.Authorization)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Bad authorization key"}`)))
		return
	}

	if workflowExecution.Status == "FINISHED" {
		log.Printf("Workflowexecution is already FINISHED. No further action can be taken")

		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Workflowexecution is already finished because of %s with status %s"}`, workflowExecution.LastNode, workflowExecution.Status)))
		return
	}

	// Not sure what's up here
	// FIXME - remove comment
	if workflowExecution.Status == "ABORTED" || workflowExecution.Status == "FAILURE" {

		if workflowExecution.Workflow.Configuration.ExitOnError {
			log.Printf("Workflowexecution already has status %s. No further action can be taken", workflowExecution.Status)
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Workflowexecution is aborted because of %s with result %s and status %s"}`, workflowExecution.LastNode, workflowExecution.Result, workflowExecution.Status)))
			return
		} else {
			log.Printf("Continuing even though it's aborted.")
		}
	}

	if actionResult.Status == "ABORTED" || actionResult.Status == "FAILURE" {
		log.Printf("Actionresult is %s. Should set workflowExecution and exit all running functions", actionResult.Status)

		newResults := []ActionResult{}
		childNodes := []string{}
		if workflowExecution.Workflow.Configuration.ExitOnError {
			workflowExecution.Status = actionResult.Status
			workflowExecution.LastNode = actionResult.Action.ID
			// Find underlying nodes and add them
		} else {
			// Finds ALL childnodes to set them to SKIPPED
			childNodes = findChildNodes(*workflowExecution, actionResult.Action.ID)
			// Remove duplicates
			log.Printf("CHILD NODES: %d", len(childNodes))
			for _, nodeId := range childNodes {
				if nodeId == actionResult.Action.ID {
					continue
				}

				// 1. Find the action itself
				// 2. Create an actionresult
				curAction := Action{ID: ""}
				for _, action := range workflowExecution.Workflow.Actions {
					if action.ID == nodeId {
						curAction = action
						break
					}
				}

				if len(curAction.ID) == 0 {
					log.Printf("Couldn't find subnode %s", nodeId)
					continue
				}

				// Check parents are done here. Only add it IF all parents are skipped
				skipNodeAdd := false
				for _, branch := range workflowExecution.Workflow.Branches {
					if branch.DestinationID == nodeId {
						// If the branch's source node is NOT in childNodes, it's not a skipped parent
						sourceNodeFound := false
						for _, item := range childNodes {
							if item == branch.SourceID {
								sourceNodeFound = true
								break
							}
						}

						if !sourceNodeFound {
							log.Printf("Not setting node %s to SKIPPED", nodeId)
							skipNodeAdd = true
							break
						}
					}
				}

				if !skipNodeAdd {
					newResult := ActionResult{
						Action:        curAction,
						ExecutionId:   actionResult.ExecutionId,
						Authorization: actionResult.Authorization,
						Result:        "Skipped because of previous node",
						StartedAt:     0,
						CompletedAt:   0,
						Status:        "SKIPPED",
					}

					newResults = append(newResults, newResult)
					increaseStatisticsField(ctx, "workflow_execution_actions_skipped", workflowExecution.Workflow.ID, 1)
				}
			}
		}

		// Cleans up aborted, and always gives a result
		lastResult := ""
		// type ActionResult struct {
		for _, result := range workflowExecution.Results {
			if result.Status == "EXECUTING" {
				result.Status = actionResult.Status
				result.Result = "Aborted because of an unknown error"
			}

			if len(result.Result) > 0 {
				lastResult = result.Result
			}

			newResults = append(newResults, result)
		}

		workflowExecution.Result = lastResult
		workflowExecution.Results = newResults

		if workflowExecution.Status == "ABORTED" {
			err = increaseStatisticsField(ctx, "workflow_executions_aborted", workflowExecution.Workflow.ID, 1)
			if err != nil {
				log.Printf("Failed to increase aborted execution stats: %s", err)
			}
		} else if workflowExecution.Status == "FAILURE" {
			err = increaseStatisticsField(ctx, "workflow_executions_failure", workflowExecution.Workflow.ID, 1)
			if err != nil {
				log.Printf("Failed to increase failure execution stats: %s", err)
			}
		}
	}

	// FIXME rebuild to be like this or something
	// workflowExecution/ExecutionId/Nodes/NodeId
	// Find the appropriate action
	if len(workflowExecution.Results) > 0 {
		// FIXME
		found := false
		outerindex := 0
		for index, item := range workflowExecution.Results {
			if item.Action.ID == actionResult.Action.ID {
				found = true
				outerindex = index
				break
			}
		}

		if found {
			// If result exists and execution variable exists, update execution value
			//log.Printf("Exec var backend: %s", workflowExecution.Results[outerindex].Action.ExecutionVariable.Name)
			actionVarName := workflowExecution.Results[outerindex].Action.ExecutionVariable.Name
			// Finds potential execution arguments
			if len(actionVarName) > 0 {
				log.Printf("EXECUTION VARIABLE LOCAL: %s", actionVarName)
				for index, execvar := range workflowExecution.ExecutionVariables {
					if execvar.Name == actionVarName {
						// Sets the value for the variable
						workflowExecution.ExecutionVariables[index].Value = actionResult.Result
						break
					}
				}
			}

			log.Printf("Updating %s in %s from %s to %s", actionResult.Action.ID, workflowExecution.ExecutionId, workflowExecution.Results[outerindex].Status, actionResult.Status)
			workflowExecution.Results[outerindex] = actionResult
		} else {
			log.Printf("Setting value of %s in %s to %s", actionResult.Action.ID, workflowExecution.ExecutionId, actionResult.Status)
			workflowExecution.Results = append(workflowExecution.Results, actionResult)
		}
	} else {
		log.Printf("Setting value of %s in %s to %s", actionResult.Action.ID, workflowExecution.ExecutionId, actionResult.Status)
		workflowExecution.Results = append(workflowExecution.Results, actionResult)
	}

	// FIXME: Have a check for skippednodes and their parents
	for resultIndex, result := range workflowExecution.Results {
		if result.Status != "SKIPPED" {
			continue
		}

		// Checks if all parents are skipped or failed. Otherwise removes them from the results
		for _, branch := range workflowExecution.Workflow.Branches {
			if branch.DestinationID == result.Action.ID {
				for _, subresult := range workflowExecution.Results {
					if subresult.Action.ID == branch.SourceID {
						if subresult.Status != "SKIPPED" && subresult.Status != "FAILURE" {
							log.Printf("SUBRESULT PARENT STATUS: %s", subresult.Status)
							log.Printf("Should remove resultIndex: %d", resultIndex)

							workflowExecution.Results = append(workflowExecution.Results[:resultIndex], workflowExecution.Results[resultIndex+1:]...)

							break
						}
					}
				}
			}
		}
	}

	extraInputs := 0
	for _, result := range workflowExecution.Results {
		if result.Action.Name == "User Input" && result.Action.AppName == "User Input" {
			extraInputs += 1
		}
	}

	//log.Printf("LENGTH: %d - %d", len(workflowExecution.Results), len(workflowExecution.Workflow.Actions))

	if len(workflowExecution.Results) == len(workflowExecution.Workflow.Actions)+extraInputs {
		finished := true
		lastResult := ""

		// Doesn't have to be SUCCESS and FINISHED everywhere anymore.
		skippedNodes := false
		for _, result := range workflowExecution.Results {
			if result.Status == "EXECUTING" {
				finished = false
				break
			}

			// FIXME: Check if ALL parents are skipped or if its just one. Otherwise execute it
			if result.Status == "SKIPPED" {
				skippedNodes = true

				// Checks if all parents are skipped or failed. Otherwise removes them from the results
				for _, branch := range workflowExecution.Workflow.Branches {
					if branch.DestinationID == result.Action.ID {
						for _, subresult := range workflowExecution.Results {
							if subresult.Action.ID == branch.SourceID {
								if subresult.Status != "SKIPPED" && subresult.Status != "FAILURE" {
									//log.Printf("SUBRESULT PARENT STATUS: %s", subresult.Status)
									//log.Printf("Should remove resultIndex: %d", resultIndex)
									finished = false
									break
								}
							}
						}
					}

					if !finished {
						break
					}
				}
			}

			lastResult = result.Result
		}

		// FIXME: Handle skip nodes - change status?
		_ = skippedNodes

		if finished {
			log.Printf("Execution of %s finished.", workflowExecution.ExecutionId)
			//log.Println("Might be finished based on length of results and everything being SUCCESS or FINISHED - VERIFY THIS. Setting status to finished.")

			workflowExecution.Result = lastResult
			workflowExecution.Status = "FINISHED"
			workflowExecution.CompletedAt = int64(time.Now().Unix())
			if workflowExecution.LastNode == "" {
				workflowExecution.LastNode = actionResult.Action.ID
			}

			err = increaseStatisticsField(ctx, "workflow_executions_success", workflowExecution.Workflow.ID, 1)
			if err != nil {
				log.Printf("Failed to increase success execution stats: %s", err)
			}
		}
	}

	// FIXME - why isn't this how it works otherwise, wtf?
	//workflow, err := getWorkflow(workflowExecution.Workflow.ID)
	//newActions := []Action{}
	//for _, action := range workflowExecution.Workflow.Actions {
	//	log.Printf("Name: %s, Env: %s", action.Name, action.Environment)
	//}

	err = setWorkflowExecution(ctx, *workflowExecution)
	if err != nil {
		log.Printf("Error saving workflow execution actionresult setting: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed setting workflowexecution actionresult: %s"}`, err)))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

func getWorkflows(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in getworkflows: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//memcacheName := fmt.Sprintf("%s_workflows", user.Username)
	ctx := context.Background()
	//if item, err := memcache.Get(ctx, memcacheName); err == memcache.ErrCacheMiss {
	//	// Not in cache
	//	//log.Printf("Workflows not in cache.")
	//} else if err != nil {
	//	log.Printf("Error getting item: %v", err)
	//} else {
	//	// FIXME - verify if value is ok? Can unmarshal etc.
	//	resp.WriteHeader(200)
	//	resp.Write(item.Value)
	//	return
	//}

	// With user, do a search for workflows with user or user's org attached
	q := datastore.NewQuery("workflow").Filter("owner =", user.Id)
	if user.Role == "admin" {
		q = datastore.NewQuery("workflow")
	}

	var workflows []Workflow
	_, err = dbclient.GetAll(ctx, q, &workflows)
	if err != nil {
		log.Printf("Failed getting workflows for user %s: %s", user.Username, err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(workflows) == 0 {
		resp.WriteHeader(200)
		resp.Write([]byte("[]"))
		return
	}

	newjson, err := json.Marshal(workflows)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflows"}`)))
		return
	}

	//item := &memcache.Item{
	//	Key:        memcacheName,
	//	Value:      newjson,
	//	Expiration: time.Minute * 10,
	//}
	//if err := memcache.Add(ctx, item); err == memcache.ErrNotStored {
	//	if err := memcache.Set(ctx, item); err != nil {
	//		log.Printf("Error setting item: %v", err)
	//	}
	//} else if err != nil {
	//	log.Printf("Error adding item: %v", err)
	//} else {
	//	//log.Printf("Set cache for %s", item.Key)
	//}

	resp.WriteHeader(200)
	resp.Write(newjson)
}

// FIXME - add to actual database etc
func setNewWorkflow(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new workflowhandler: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	var workflow Workflow
	err = json.Unmarshal(body, &workflow)
	if err != nil {
		log.Printf("Failed unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	workflow.ID = uuid.NewV4().String()
	workflow.Owner = user.Id
	workflow.Sharing = "private"

	ctx := context.Background()
	log.Printf("Saved new workflow %s with name %s", workflow.ID, workflow.Name)
	err = increaseStatisticsField(ctx, "total_workflows", workflow.ID, 1)
	if err != nil {
		log.Printf("Failed to increase total workflows stats: %s", err)
	}

	if len(workflow.Actions) == 0 {
		workflow.Actions = []Action{}
	}
	if len(workflow.Branches) == 0 {
		workflow.Branches = []Branch{}
	}
	if len(workflow.Triggers) == 0 {
		workflow.Triggers = []Trigger{}
	}
	if len(workflow.Errors) == 0 {
		workflow.Errors = []string{}
	}

	newActions := []Action{}
	for _, action := range workflow.Actions {
		if action.Environment == "" {
			//action.Environment = baseEnvironment
			action.IsValid = true
		}

		newActions = append(newActions, action)
	}

	// Initialized without functions = adding a hello world node.
	if len(newActions) == 0 {
		log.Printf("APPENDING NEW APP FOR NEW WORKFLOW")

		// Adds the Testing app if it's a new workflow
		workflowapps, err := getAllWorkflowApps(ctx)
		if err == nil {
			// FIXME: Add real env
			envName := "Shuffle"
			environments, err := getEnvironments(ctx)
			if err == nil {
				for _, env := range environments {
					if env.Default {
						envName = env.Name
						break
					}
				}
			}

			for _, item := range workflowapps {
				if item.Name == "Testing" && item.AppVersion == "1.0.0" {
					nodeId := "40447f30-fa44-4a4f-a133-4ee710368737"
					workflow.Start = nodeId
					newActions = append(newActions, Action{
						Label:       "Start node",
						Name:        "hello_world",
						Environment: envName,
						Parameters:  []WorkflowAppActionParameter{},
						Position: struct {
							X float64 "json:\"x\" datastore:\"x\""
							Y float64 "json:\"y\" datastore:\"y\""
						}{X: 449.5, Y: 446},
						Priority:    0,
						Errors:      []string{},
						ID:          nodeId,
						IsValid:     true,
						IsStartNode: true,
						Sharing:     true,
						PrivateID:   "",
						SmallImage:  "",
						AppName:     item.Name,
						AppVersion:  item.AppVersion,
						AppID:       item.ID,
						LargeImage:  item.LargeImage,
					})
					break
				}
			}
		}
	} else {
		log.Printf("Has %d actions already", len(newActions))
	}

	workflow.Actions = newActions
	workflow.IsValid = true
	workflow.Configuration.ExitOnError = false

	workflowjson, err := json.Marshal(workflow)
	if err != nil {
		log.Printf("Failed workflow json setting marshalling: %s", err)
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	err = setWorkflow(ctx, workflow, workflow.ID)
	if err != nil {
		log.Printf("Failed setting workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//memcacheName := fmt.Sprintf("%s_workflows", user.Username)
	//memcache.Delete(ctx, memcacheName)

	resp.WriteHeader(200)
	//log.Println(string(workflowjson))
	resp.Write(workflowjson)
}

func deleteWorkflow(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in deleting workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to delete is not valid"}`))
		return
	}

	ctx := context.Background()
	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (delete workflow): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - have a check for org etc too..
	if user.Id != workflow.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Clean up triggers and executions
	for _, item := range workflow.Triggers {
		if item.TriggerType == "SCHEDULE" && item.Status != "uninitialized" {
			err = deleteSchedule(ctx, item.ID)
			if err != nil {
				log.Printf("Failed to delete schedule: %s - is it started?", err)
			}
		} else if item.TriggerType == "WEBHOOK" {
			//err = removeWebhookFunction(ctx, item.ID)
			//if err != nil {
			//	log.Printf("Failed to delete webhook: %s", err)
			//}
		} else if item.TriggerType == "EMAIL" {
			err = handleOutlookSubRemoval(ctx, workflow.ID, item.ID)
			if err != nil {
				log.Printf("Failed to delete email sub: %s", err)
			}
		}

		err = increaseStatisticsField(ctx, "total_workflow_triggers", workflow.ID, -1)
		if err != nil {
			log.Printf("Failed to increase total workflows: %s", err)
		}
	}

	// FIXME - maybe delete workflow executions
	log.Printf("Should delete workflow %s", fileId)
	err = DeleteKey(ctx, "workflow", fileId)
	if err != nil {
		log.Printf("Failed deleting key %s", fileId)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Failed deleting key"}`))
		return
	}

	err = increaseStatisticsField(ctx, "total_workflows", fileId, -1)
	if err != nil {
		log.Printf("Failed to increase total workflows: %s", err)
	}

	//memcacheName := fmt.Sprintf("%s_%s", user.Username, fileId)
	//memcache.Delete(ctx, memcacheName)
	//memcacheName = fmt.Sprintf("%s_workflows", user.Username)
	//memcache.Delete(ctx, memcacheName)

	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true}`))
}

// Adds app auth tracking
func updateAppAuth(auth AppAuthenticationStorage, workflowId, nodeId string, add bool) error {
	workflowFound := false
	workflowIndex := 0
	nodeFound := false
	for index, workflow := range auth.Usage {
		if workflow.WorkflowId == workflowId {
			// Check if node exists
			workflowFound = true
			workflowIndex = index
			for _, actionId := range workflow.Nodes {
				if actionId == nodeId {
					nodeFound = true
					break
				}
			}

			break
		}
	}

	// FIXME: Add a way to use !add to remove
	updateAuth := false
	if !workflowFound && add {
		log.Printf("Adding workflow things to auth!")
		usageItem := AuthenticationUsage{
			WorkflowId: workflowId,
			Nodes:      []string{nodeId},
		}

		auth.Usage = append(auth.Usage, usageItem)
		auth.WorkflowCount += 1
		auth.NodeCount += 1
		updateAuth = true
	} else if !nodeFound && add {
		log.Printf("Adding node things to auth!")
		auth.Usage[workflowIndex].Nodes = append(auth.Usage[workflowIndex].Nodes, nodeId)
		auth.NodeCount += 1
		updateAuth = true
	}

	if updateAuth {
		log.Printf("Updating auth!")
		ctx := context.Background()
		err := setWorkflowAppAuthDatastore(ctx, auth, auth.Id)
		if err != nil {
			log.Printf("Failed setting up app auth %s: %s", auth.Id, err)
			return err
		}
	}

	return nil
}

// Saves a workflow to an ID
func saveWorkflow(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	//log.Println("Start")
	user, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in edit workflow: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//log.Println("PostUser")
	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		log.Printf(`ID %s is not valid`, fileId)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to save is not valid"}`))
		return
	}

	// Here to check access rights
	ctx := context.Background()
	log.Println("GetWorkflow start")

	tmpworkflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (save workflow): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Println("GetWorkflow end")

	// FIXME - have a check for org etc too..
	if user.Id != tmpworkflow.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s (save)", user.Username, tmpworkflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//Actions           []Action   `json:"actions" datastore:"actions,noindex"`

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Failed hook unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	var workflow Workflow
	err = json.Unmarshal([]byte(body), &workflow)
	//log.Printf(string(body))
	if err != nil {
		log.Printf("Failed workflow unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - auth and check if they should have access
	if fileId != workflow.ID {
		log.Printf("Path and request ID are not matching: %s:%s.", fileId, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Fixing wrong owners when importing
	if workflow.Owner == "" {
		workflow.Owner = user.Id
	}

	// FIXME - this shouldn't be necessary with proper API checks
	newActions := []Action{}
	allNodes := []string{}

	//log.Printf("Action: %#v", action.Authentication)
	for _, action := range workflow.Actions {
		allNodes = append(allNodes, action.ID)

		if action.Environment == "" {
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "An environment for %s is required"}`, action.Label)))
			return
			action.IsValid = true
		}

		// FIXME: Have a good way of tracking errors. ID's or similar.
		if !action.IsValid {
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Node %s is invalid and needs to be remade."}`, action.Label)))
			return
			action.IsValid = true
			action.Errors = []string{}
		}

		newActions = append(newActions, action)
	}

	workflow.Actions = newActions

	for _, trigger := range workflow.Triggers {
		log.Printf("Trigger %s: %s", trigger.TriggerType, trigger.Status)

		//log.Println("TRIGGERS")
		allNodes = append(allNodes, trigger.ID)
	}

	if len(workflow.Actions) == 0 {
		workflow.Actions = []Action{}
	}
	if len(workflow.Branches) == 0 {
		workflow.Branches = []Branch{}
	}
	if len(workflow.Triggers) == 0 {
		workflow.Triggers = []Trigger{}
	}
	if len(workflow.Errors) == 0 {
		workflow.Errors = []string{}
	}

	if len(workflow.ExecutionVariables) > 0 {
		log.Printf("Found %d execution variable(s)", len(workflow.ExecutionVariables))
	}

	if len(workflow.WorkflowVariables) > 0 {
		log.Printf("Found %d workflow variable(s)", len(workflow.WorkflowVariables))
	}

	// FIXME - do actual checks ROFL
	// FIXME - minor issues with e.g. hello world and self.console_logger
	// Nodechecks
	foundNodes := []string{}
	for _, node := range allNodes {
		for _, branch := range workflow.Branches {
			//log.Println("branch")
			//log.Println(node)
			//log.Println(branch.DestinationID)
			if node == branch.DestinationID || node == branch.SourceID {
				foundNodes = append(foundNodes, node)
				break
			}
		}
	}

	// FIXME - append all nodes (actions, triggers etc) to one single array here
	if len(foundNodes) != len(allNodes) || len(workflow.Actions) <= 0 {
		// This shit takes a few seconds lol
		if !workflow.IsValid {
			oldworkflow, err := getWorkflow(ctx, fileId)
			if err != nil {
				log.Printf("Workflow %s doesn't exist - oldworkflow.", fileId)
				resp.WriteHeader(401)
				resp.Write([]byte(`{"success": false, "reason": "Item already exists."}`))
				return
			}

			oldworkflow.IsValid = false
			err = setWorkflow(ctx, *oldworkflow, fileId)
			if err != nil {
				log.Printf("Failed saving workflow to database: %s", err)
				resp.WriteHeader(401)
				resp.Write([]byte(`{"success": false}`))
				return
			}
		}

		// FIXME - more checks here - force reload of data or something
		//if len(allNodes) == 0 {
		//	resp.WriteHeader(401)
		//	resp.Write([]byte(`{"success": false, "reason": "Please insert a node"}`))
		//	return
		//}

		// Allowed with only a start node
		//if len(allNodes) != 1 {
		//	resp.WriteHeader(401)
		//	resp.Write([]byte(`{"success": false, "reason": "There are nodes with no branches"}`))
		//	return
		//}
	}

	// FIXME - might be a sploit to run someone elses app if getAllWorkflowApps
	// doesn't check sharing=true
	// Have to do it like this to add the user's apps
	log.Println("Apps set starting")
	//log.Printf("EXIT ON ERROR: %#v", workflow.Configuration.ExitOnError)
	workflowApps := []WorkflowApp{}
	//memcacheName = "all_apps"
	//if item, err := memcache.Get(ctx, memcacheName); err == memcache.ErrCacheMiss {
	//	// Not in cache
	//	log.Printf("Apps not in cache.")
	workflowApps, err = getAllWorkflowApps(ctx)
	if err != nil {
		log.Printf("Failed getting all workflow apps from database: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Started getting the single apps, but if it's weird, this is faster
	// 1. Check workflow.Start
	// 2. Check if any node has "isStartnode"
	if len(workflow.Actions) > 0 {
		index := -1
		for indexFound, action := range workflow.Actions {
			//log.Println("Apps set done")
			if workflow.Start == action.ID {
				index = indexFound
			}
		}

		if index >= 0 {
			workflow.Actions[0].IsStartNode = true
		} else {
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "You need to set a startnode."}`)))
			return
		}
	}

	allAuths, err := getAllWorkflowAppAuth(ctx)
	if userErr != nil {
		log.Printf("Api authentication failed in get all apps: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Check every app action and param to see whether they exist
	newActions = []Action{}
	for _, action := range workflow.Actions {
		reservedApps := []string{
			"0ca8887e-b4af-4e3e-887c-87e9d3bc3d3e",
		}

		//log.Printf("%s Action execution var: %s", action.Label, action.ExecutionVariable.Name)

		builtin := false
		for _, id := range reservedApps {
			if id == action.AppID {
				builtin = true
				break
			}
		}

		// Check auth
		// 1. Find the auth in question
		// 2. Update the node and workflow info in the auth
		// 3. Get the values in the auth and add them to the action values
		if len(action.AuthenticationId) > 0 {
			authFound := false
			for _, auth := range allAuths {
				if auth.Id == action.AuthenticationId {
					authFound = true

					// Updates the auth item itself IF necessary
					go updateAppAuth(auth, workflow.ID, action.ID, true)
					break
				}
			}

			if !authFound {
				log.Printf("App auth %s doesn't exist", action.AuthenticationId)
				resp.WriteHeader(401)
				resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "App auth %s doesn't exist"}`, action.AuthenticationId)))
				return
			}
		}

		if builtin {
			newActions = append(newActions, action)
		} else {
			curapp := WorkflowApp{}
			// FIXME - can this work with ONLY AppID?
			for _, app := range workflowApps {
				if app.ID == action.AppID {
					curapp = app
					break
				}

				// Has to NOT be generated
				if app.Name == action.AppName && app.AppVersion == action.AppVersion {
					curapp = app
					break
				}
			}

			// Check to see if the whole app is valid
			if curapp.Name != action.AppName {
				log.Printf("App %s doesn't exist.", action.AppName)
				resp.WriteHeader(401)
				resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "App %s doesn't exist"}`, action.AppName)))
				return
			}

			// Check tosee if the appaction is valid
			curappaction := WorkflowAppAction{}
			for _, curAction := range curapp.Actions {
				if action.Name == curAction.Name {
					curappaction = curAction
					break
				}
			}

			// Check to see if the action is valid
			if curappaction.Name != action.Name {
				log.Printf("Appaction %s doesn't exist.", action.Name)
				resp.WriteHeader(401)
				resp.Write([]byte(`{"success": false}`))
				return
			}

			// FIXME - check all parameters to see if they're valid
			// Includes checking required fields

			newParams := []WorkflowAppActionParameter{}
			for _, param := range curappaction.Parameters {
				found := false

				// Handles check for parameter exists + value not empty in used fields
				for _, actionParam := range action.Parameters {
					if actionParam.Name == param.Name {
						found = true

						if actionParam.Value == "" && actionParam.Variant == "STATIC_VALUE" && actionParam.Required == true {
							log.Printf("Appaction %s with required param '%s' is empty.", action.Name, param.Name)
							resp.WriteHeader(401)
							resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Appaction %s with required param '%s' is empty."}`, action.Name, param.Name)))
							return

						}

						if actionParam.Variant == "" {
							actionParam.Variant = "STATIC_VALUE"
						}

						newParams = append(newParams, actionParam)
						break
					}
				}

				// Handles check for required params
				if !found && param.Required {
					log.Printf("Appaction %s with required param %s doesn't exist.", action.Name, param.Name)
					resp.WriteHeader(401)
					resp.Write([]byte(`{"success": false}`))
					return
				}

			}

			action.Parameters = newParams
			newActions = append(newActions, action)
		}
	}

	workflow.Actions = newActions
	workflow.IsValid = true
	log.Printf("Tags: %#v", workflow.Tags)

	err = setWorkflow(ctx, workflow, fileId)
	if err != nil {
		log.Printf("Failed saving workflow to database: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	totalOldActions := len(tmpworkflow.Actions)
	totalNewActions := len(workflow.Actions)
	err = increaseStatisticsField(ctx, "total_workflow_actions", workflow.ID, int64(totalNewActions-totalOldActions))
	if err != nil {
		log.Printf("Failed to change total actions data: %s", err)
	}

	log.Printf("Saved new version of workflow %s (%s)", workflow.Name, fileId)
	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true}`))
}

func getWorkflowLocal(fileId string, request *http.Request) ([]byte, error) {
	fullUrl := fmt.Sprintf("%s/api/v1/workflows/%s", localBase, fileId)
	client := &http.Client{}
	req, err := http.NewRequest(
		"GET",
		fullUrl,
		nil,
	)

	if err != nil {
		return []byte{}, err
	}

	for key, value := range request.Header {
		req.Header.Add(key, strings.Join(value, ";"))
	}

	newresp, err := client.Do(req)
	if err != nil {
		return []byte{}, err
	}

	body, err := ioutil.ReadAll(newresp.Body)
	if err != nil {
		return []byte{}, err
	}

	// Temporary solution
	if strings.Contains(string(body), "reason") && strings.Contains(string(body), "false") {
		return []byte{}, errors.New(fmt.Sprintf("Failed getting workflow %s with message %s", fileId, string(body)))
	}

	return body, nil
}

func abortExecution(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in abort workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to abort is not valid"}`))
		return
	}

	executionId := location[6]
	if len(executionId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "ExecutionID not valid"}`))
		return
	}

	ctx := context.Background()
	workflowExecution, err := getWorkflowExecution(ctx, executionId)
	if err != nil {
		log.Printf("Failed getting execution (abort) %s: %s", executionId, err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed getting execution ID %s because it doesn't exist (abort)."}`, executionId)))
		return
	}

	// FIXME - have a check for org etc too..
	if user.Id != workflowExecution.Workflow.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflowexecution workflow %s", user.Username, workflowExecution.Workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if workflowExecution.Status == "ABORTED" || workflowExecution.Status == "FAILURE" || workflowExecution.Status == "FINISHED" {
		log.Printf("Stopped execution of %s with status %s", executionId, workflowExecution.Status)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Status for %s is %s, which can't be aborted."}`, executionId, workflowExecution.Status)))
		return
	}

	topic := "workflowexecution"

	workflowExecution.CompletedAt = int64(time.Now().Unix())
	workflowExecution.Status = "ABORTED"

	lastResult := ""
	newResults := []ActionResult{}
	// type ActionResult struct {
	for _, result := range workflowExecution.Results {
		if result.Status == "EXECUTING" {
			result.Status = "ABORTED"
			result.Result = "Aborted because of an unknown error"
		}

		if len(result.Result) > 0 {
			lastResult = result.Result
		}

		newResults = append(newResults, result)
	}

	workflowExecution.Results = newResults
	if len(workflowExecution.Result) == 0 {
		workflowExecution.Result = lastResult
	}

	err = setWorkflowExecution(ctx, *workflowExecution)
	if err != nil {
		log.Printf("Error saving workflow execution for updates when aborting %s: %s", topic, err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed setting workflowexecution status to abort"}`)))
		return
	}

	err = increaseStatisticsField(ctx, "workflow_executions_aborted", workflowExecution.Workflow.ID, 1)
	if err != nil {
		log.Printf("Failed to increase aborted execution stats: %s", err)
	}

	// FIXME - allowed to edit it? idk
	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))

	// Not sure what's up here
	//if workflowExecution.Status == "ABORTED" || workflowExecution.Status == "FAILURE" {
	//	log.Printf("Workflowexecution is already aborted. No further action can be taken")
	//	resp.WriteHeader(401)
	//	resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Workflowexecution is aborted because of %s with result %s and status %s"}`, workflowExecution.LastNode, workflowExecution.Result, workflowExecution.Status)))
	//	return
	//}
}

//// New execution with firestore

func cleanupExecutions(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in execute workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "message": "Not authenticated"}`))
		return
	}

	//if user.Role != "admin" {
	//	resp.WriteHeader(401)
	//	resp.Write([]byte(`{"success": false, "message": "Insufficient permissions"}`))
	//	return
	//}

	log.Printf("CLEANUP!")
	log.Printf("%#v", user)

	ctx := context.Background()
	// Removes three months from today
	timestamp := int64(time.Now().AddDate(0, -2, 0).Unix())
	log.Println(timestamp)
	q := datastore.NewQuery("workflowexecution").Filter("started_at <", timestamp)
	var workflowExecutions []WorkflowExecution
	_, err = dbclient.GetAll(ctx, q, &workflowExecutions)
	if err != nil {
		log.Printf("Error getting workflowexec (cleanup): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed getting all workflowexecutions"}`)))
		return
	}

	log.Println(len(workflowExecutions))

	resp.WriteHeader(200)
	resp.Write([]byte("OK"))
}

func handleExecution(id string, workflow Workflow, request *http.Request) (WorkflowExecution, string, error) {
	ctx := context.Background()
	if workflow.ID == "" || workflow.ID != id {
		tmpworkflow, err := getWorkflow(ctx, id)
		if err != nil {
			log.Printf("Failed getting the workflow locally (execution cleanup): %s", err)
			return WorkflowExecution{}, "Failed getting workflow", err
		}

		workflow = *tmpworkflow
	}

	if len(workflow.Actions) == 0 {
		workflow.Actions = []Action{}
	}
	if len(workflow.Branches) == 0 {
		workflow.Branches = []Branch{}
	}
	if len(workflow.Triggers) == 0 {
		workflow.Triggers = []Trigger{}
	}
	if len(workflow.Errors) == 0 {
		workflow.Errors = []string{}
	}

	if !workflow.IsValid {
		log.Printf("Stopped execution as workflow %s is not valid.", workflow.ID)
		return WorkflowExecution{}, fmt.Sprintf(`workflow %s is invalid`, workflow.ID), errors.New("Failed getting workflow")
	}

	workflowBytes, err := json.Marshal(workflow)
	if err != nil {
		log.Printf("Failed workflow unmarshal in execution: %s", err)
		return WorkflowExecution{}, "", err
	}

	//log.Println(workflow)
	var workflowExecution WorkflowExecution
	err = json.Unmarshal(workflowBytes, &workflowExecution.Workflow)
	if err != nil {
		log.Printf("Failed execution unmarshaling: %s", err)
		return WorkflowExecution{}, "Failed unmarshal during execution", err
	}

	makeNew := true
	if request.Method == "POST" {
		body, err := ioutil.ReadAll(request.Body)
		if err != nil {
			log.Printf("Failed request POST read: %s", err)
			return WorkflowExecution{}, "Failed getting body", err
		}

		// This one doesn't really matter.
		var execution ExecutionRequest
		err = json.Unmarshal(body, &execution)
		if err != nil {
			log.Printf("Failed execution POST unmarshaling - continuing anyway: %s", err)
			//return WorkflowExecution{}, "", err
		}

		if execution.Start == "" && len(body) > 0 {
			execution.ExecutionArgument = string(body)
		}

		// FIXME - this should have "execution_argument" from executeWorkflow frontend
		//log.Printf("EXEC: %#v", execution)
		if len(execution.ExecutionArgument) > 0 {
			workflowExecution.ExecutionArgument = execution.ExecutionArgument
		}

		if len(execution.ExecutionSource) > 0 {
			workflowExecution.ExecutionSource = execution.ExecutionSource
		}

		//log.Printf("Execution data: %#v", execution)
		if len(execution.Start) == 36 {
			log.Printf("[INFO] Should start execution on node %s", execution.Start)
			workflowExecution.Start = execution.Start

			found := false
			for _, action := range workflow.Actions {
				if action.ID == workflow.Start {
					found = true
				}
			}

			if !found {
				log.Printf("[ERROR] ACTION %s WAS NOT FOUND!", workflow.Start)
				return WorkflowExecution{}, fmt.Sprintf("Startnode %s was not found in actions", workflow.Start), errors.New(fmt.Sprintf("Startnode %s was not found in actions", workflow.Start))
			}
		} else if len(execution.Start) > 0 {

			log.Printf("[ERROR] START ACTION %s IS WRONG ID LENGTH %d!", execution.Start, len(execution.Start))
			return WorkflowExecution{}, fmt.Sprintf("Startnode %s was not found in actions", execution.Start), errors.New(fmt.Sprintf("Startnode %s was not found in actions", execution.Start))
		}

		if len(execution.ExecutionId) == 36 {
			workflowExecution.ExecutionId = execution.ExecutionId
		} else {
			sessionToken := uuid.NewV4()
			workflowExecution.ExecutionId = sessionToken.String()
		}
	} else {
		// Check for parameters of start and ExecutionId
		start, startok := request.URL.Query()["start"]
		answer, answerok := request.URL.Query()["answer"]
		referenceId, referenceok := request.URL.Query()["reference_execution"]
		if answerok && referenceok {
			// If answer is false, reference execution with result
			log.Printf("Answer is OK AND reference is OK!")
			if answer[0] == "false" {
				log.Printf("Should update reference and return, no need for further execution!")

				// Get the reference execution
				oldExecution, err := getWorkflowExecution(ctx, referenceId[0])
				if err != nil {
					log.Printf("Failed getting execution (execution) %s: %s", referenceId[0], err)
					return WorkflowExecution{}, fmt.Sprintf("Failed getting execution ID %s because it doesn't exist.", referenceId[0]), err
				}

				if oldExecution.Workflow.ID != id {
					log.Println("Wrong workflowid!")
					return WorkflowExecution{}, fmt.Sprintf("Bad ID %s", referenceId), errors.New("Bad ID")
				}

				newResults := []ActionResult{}
				//log.Printf("%#v", oldExecution.Results)
				for _, result := range oldExecution.Results {
					log.Printf("%s - %s", result.Action.ID, start[0])
					if result.Action.ID == start[0] {
						note, noteok := request.URL.Query()["note"]
						if noteok {
							result.Result = fmt.Sprintf("User note: %s", note[0])
						} else {
							result.Result = fmt.Sprintf("User clicked %s", answer[0])
						}

						// Stopping the whole thing
						result.CompletedAt = int64(time.Now().Unix())
						result.Status = "ABORTED"
						oldExecution.Status = result.Status
						oldExecution.Result = result.Result
						oldExecution.LastNode = result.Action.ID
					}

					newResults = append(newResults, result)
				}

				oldExecution.Results = newResults
				err = setWorkflowExecution(ctx, *oldExecution)
				if err != nil {
					log.Printf("Error saving workflow execution actionresult setting: %s", err)
					return WorkflowExecution{}, fmt.Sprintf("Failed setting workflowexecution actionresult in execution: %s", err), err
				}

				return WorkflowExecution{}, "", nil
			}
		}

		if referenceok {
			log.Printf("Handling an old execution continuation!")
			// Will use the old name, but still continue with NEW ID
			oldExecution, err := getWorkflowExecution(ctx, referenceId[0])
			if err != nil {
				log.Printf("Failed getting execution (execution) %s: %s", referenceId[0], err)
				return WorkflowExecution{}, fmt.Sprintf("Failed getting execution ID %s because it doesn't exist.", referenceId[0]), err
			}

			workflowExecution = *oldExecution
		}

		if len(workflowExecution.ExecutionId) == 0 {
			sessionToken := uuid.NewV4()
			workflowExecution.ExecutionId = sessionToken.String()
		} else {
			log.Printf("Using the same executionId as before: %s", workflowExecution.ExecutionId)
			makeNew = false
		}

		// Don't override workflow defaults
		if startok {
			log.Printf("Setting start to %s based on query!", start[0])
			//workflowExecution.Workflow.Start = start[0]
			workflowExecution.Start = start[0]
		}

	}

	// FIXME - regex uuid, and check if already exists?
	if len(workflowExecution.ExecutionId) != 36 {
		log.Printf("Invalid uuid: %s", workflowExecution.ExecutionId)
		return WorkflowExecution{}, "Invalid uuid", err
	}

	// FIXME - find owner of workflow
	// FIXME - get the actual workflow itself and build the request
	// MAYBE: Don't send the workflow within the pubsub, as this requires more data to be sent
	// Check if a worker already exists for company, else run one with:
	// locations, project IDs and subscription names

	// When app is executed:
	// Should update with status execution (somewhere), which will trigger the next node
	// IF action.type == internal, we need the internal watcher to be running and executing
	// This essentially means the WORKER has to be the responsible party for new actions in the INTERNAL landscape
	// Results are ALWAYS posted back to cloud@execution_id?
	if makeNew {
		workflowExecution.Type = "workflow"
		//workflowExecution.Stream = "tmp"
		//workflowExecution.WorkflowQueue = "tmp"
		//workflowExecution.SubscriptionNameNodestream = "testcompany-nodestream"
		workflowExecution.ProjectId = gceProject
		workflowExecution.Locations = []string{"europe-west2"}
		workflowExecution.WorkflowId = workflow.ID
		workflowExecution.StartedAt = int64(time.Now().Unix())
		workflowExecution.CompletedAt = 0
		workflowExecution.Authorization = uuid.NewV4().String()

		// Status for the entire workflow.
		workflowExecution.Status = "EXECUTING"
	}

	if len(workflowExecution.ExecutionSource) == 0 {
		log.Printf("[INFO] No execution source (trigger) specified. Setting to default")
		workflowExecution.ExecutionSource = "default"
	} else {
		log.Printf("[INFO] Execution source is %s for execution ID %s", workflowExecution.ExecutionSource, workflowExecution.ExecutionId)
	}

	workflowExecution.ExecutionVariables = workflow.ExecutionVariables
	// Local authorization for this single workflow used in workers.

	// FIXME: Used for cloud
	//mappedData, err := json.Marshal(workflowExecution)
	//if err != nil {
	//	log.Printf("Failed workflowexecution marshalling: %s", err)
	//	resp.WriteHeader(http.StatusInternalServerError)
	//	resp.Write([]byte(`{"success": false}`))
	//	return
	//}

	//log.Println(string(mappedData))

	if len(workflowExecution.Start) == 0 {
		workflowExecution.Start = workflowExecution.Workflow.Start
	}
	log.Printf("[INFO] New startnode: %s", workflowExecution.Start)

	childNodes := findChildNodes(workflowExecution, workflowExecution.Start)

	topic := "workflows"
	startFound := false
	// FIXME - remove this?
	newActions := []Action{}
	defaultResults := []ActionResult{}

	allAuths := []AppAuthenticationStorage{}
	for _, action := range workflowExecution.Workflow.Actions {
		action.LargeImage = ""
		if action.ID == workflowExecution.Start {
			startFound = true
		}
		//log.Println(action.Environment)

		if action.Environment == "" {
			return WorkflowExecution{}, fmt.Sprintf("Environment is not defined for %s", action.Name), errors.New("Environment not defined!")
		}

		// FIXME: Authentication parameters
		if len(action.AuthenticationId) > 0 {
			if len(allAuths) == 0 {
				allAuths, err = getAllWorkflowAppAuth(ctx)
				if err != nil {
					log.Printf("Api authentication failed in get all app auth: %s", err)
					return WorkflowExecution{}, fmt.Sprintf("Api authentication failed in get all app auth: %s", err), err
				}
			}

			curAuth := AppAuthenticationStorage{Id: ""}
			for _, auth := range allAuths {
				if auth.Id == action.AuthenticationId {
					curAuth = auth
					break
				}
			}

			if len(curAuth.Id) == 0 {
				return WorkflowExecution{}, fmt.Sprintf("Auth ID %s doesn't exist", action.AuthenticationId), errors.New(fmt.Sprintf("Auth ID %s doesn't exist", action.AuthenticationId))
			}

			// Rebuild params with the right data. This is to prevent issues on the frontend
			newParams := []WorkflowAppActionParameter{}
			for _, param := range action.Parameters {

				for _, authparam := range curAuth.Fields {
					if param.Name == authparam.Key {
						log.Printf("Name: %s - value: %s", param.Name, param.Value)
						param.Value = authparam.Value
						log.Printf("Name: %s - value: %s\n", param.Name, param.Value)
						break
					}
				}

				newParams = append(newParams, param)
			}

			action.Parameters = newParams
		}

		newActions = append(newActions, action)

		// If the node is NOT found, it's supposed to be set to SKIPPED,
		// as it's not a childnode of the startnode
		// This is a configuration item for the workflow itself.
		if !workflowExecution.Workflow.Configuration.StartFromTop {
			found := false
			for _, nodeId := range childNodes {
				if nodeId == action.ID {
					//log.Printf("Found %s", action.ID)
					found = true
				}
			}

			if !found {
				if action.ID == workflowExecution.Start {
					continue
				}

				log.Printf("Should set %s to SKIPPED as it's NOT a childnode of the startnode.", action.ID)
				defaultResults = append(defaultResults, ActionResult{
					Action:        action,
					ExecutionId:   workflowExecution.ExecutionId,
					Authorization: workflowExecution.Authorization,
					Result:        "Skipped because it's not under the startnode",
					StartedAt:     0,
					CompletedAt:   0,
					Status:        "SKIPPED",
				})
			}
		}
	}

	if !startFound {
		log.Printf("Startnode %s doesn't exist!", workflowExecution.Start)
		return WorkflowExecution{}, fmt.Sprintf("Workflow action %s doesn't exist in workflow", workflowExecution.Start), errors.New(fmt.Sprintf("Workflow start node %s doesn't exist. Exiting!", workflowExecution.Start))
	}

	// Verification for execution environments
	workflowExecution.Results = defaultResults
	workflowExecution.Workflow.Actions = newActions
	onpremExecution := false
	environments := []string{}

	// Check if the actions are children of the startnode?
	imageNames := []string{}
	for _, action := range workflowExecution.Workflow.Actions {
		if action.Environment != cloudname {
			found := false
			for _, env := range environments {
				if env == action.Environment {
					found = true
					break
				}
			}

			// Check if the app exists?
			newName := action.AppName
			newName = strings.ReplaceAll(newName, " ", "-")
			imageNames = append(imageNames, fmt.Sprintf("%s:%s_%s", baseDockerName, newName, action.AppVersion))

			if !found {
				environments = append(environments, action.Environment)
			}

			onpremExecution = true
		}
	}

	err = imageCheckBuilder(imageNames)
	if err != nil {
		log.Printf("[ERROR] Failed building the required images from %#v: %s", imageNames, err)
		return WorkflowExecution{}, "Failed building missing Docker images", err
	}

	err = setWorkflowExecution(ctx, workflowExecution)
	if err != nil {
		log.Printf("Error saving workflow execution for updates %s: %s", topic, err)
		return WorkflowExecution{}, "Failed getting workflowexecution", err
	}

	// Adds queue for onprem execution
	// FIXME - add specifics to executionRequest, e.g. specific environment (can run multi onprem)
	if onpremExecution {
		// FIXME - tmp name based on future companyname-companyId
		for _, environment := range environments {
			log.Printf("[INFO] Execution: %s should execute onprem with execution environment \"%s\"", workflowExecution.ExecutionId, environment)

			executionRequest := ExecutionRequest{
				ExecutionId:   workflowExecution.ExecutionId,
				WorkflowId:    workflowExecution.Workflow.ID,
				Authorization: workflowExecution.Authorization,
				Environments:  environments,
			}

			executionRequestWrapper, err := getWorkflowQueue(ctx, environment)
			if err != nil {
				executionRequestWrapper = ExecutionRequestWrapper{
					Data: []ExecutionRequest{executionRequest},
				}
			} else {
				executionRequestWrapper.Data = append(executionRequestWrapper.Data, executionRequest)
			}

			//log.Printf("Execution request: %#v", executionRequest)
			err = setWorkflowQueue(ctx, executionRequestWrapper, environment)
			if err != nil {
				log.Printf("Failed adding to db: %s", err)
			}
		}
	} else {
		log.Printf("[ERROR] Cloud not implemented yet")
	}

	err = increaseStatisticsField(ctx, "workflow_executions", workflow.ID, 1)
	if err != nil {
		log.Printf("Failed to increase stats execution stats: %s", err)
	}

	return workflowExecution, "", nil
}

func executeWorkflow(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in execute workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to execute is not valid"}`))
		return
	}

	//memcacheName := fmt.Sprintf("%s_%s", user.Username, fileId)
	ctx := context.Background()
	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (execute workflow): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - have a check for org etc too..
	// FIXME - admin check like this? idk
	if user.Id != workflow.Owner && user.Role != "admin" && user.Role != "scheduler" && user.Role != fmt.Sprintf("workflow_%s", fileId) {
		log.Printf("Wrong user (%s) for workflow %s (execute)", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Printf("[INFO] Starting execution of %s!", fileId)
	workflowExecution, executionResp, err := handleExecution(fileId, *workflow, request)

	if err == nil {
		resp.WriteHeader(200)
		resp.Write([]byte(fmt.Sprintf(`{"success": true, "execution_id": "%s", "authorization": "%s"}`, workflowExecution.ExecutionId, workflowExecution.Authorization)))
		return
	}

	resp.WriteHeader(500)
	resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, executionResp)))
}

func stopSchedule(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in schedule workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	var scheduleId string
	if location[1] == "api" {
		if len(location) <= 6 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
		scheduleId = location[6]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to stop schedule is not valid"}`))
		return
	}

	if len(scheduleId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Schedule ID not valid"}`))
		return
	}

	ctx := context.Background()
	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (stop schedule): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - have a check for org etc too..
	// FIXME - admin check like this? idk
	if user.Id != workflow.Owner && user.Role != "admin" && user.Role != "scheduler" {
		log.Printf("Wrong user (%s) for workflow %s (stop schedule)", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	err = deleteSchedule(ctx, scheduleId)
	if err != nil {
		if strings.Contains(err.Error(), "Job not found") {
			resp.WriteHeader(200)
			resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
		} else {
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed stopping schedule"}`)))
		}
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
	return
}

func stopScheduleGCP(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in schedule workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	var scheduleId string
	if location[1] == "api" {
		if len(location) <= 6 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
		scheduleId = location[6]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to stop schedule is not valid"}`))
		return
	}

	if len(scheduleId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Schedule ID not valid"}`))
		return
	}

	ctx := context.Background()
	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (stop schedule GCP): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - have a check for org etc too..
	// FIXME - admin check like this? idk
	if user.Id != workflow.Owner && user.Role != "admin" && user.Role != "scheduler" {
		log.Printf("Wrong user (%s) for workflow %s (stop schedule)", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(workflow.Actions) == 0 {
		workflow.Actions = []Action{}
	}
	if len(workflow.Branches) == 0 {
		workflow.Branches = []Branch{}
	}
	if len(workflow.Triggers) == 0 {
		workflow.Triggers = []Trigger{}
	}
	if len(workflow.Errors) == 0 {
		workflow.Errors = []string{}
	}

	err = deleteSchedule(ctx, scheduleId)
	if err != nil {
		if strings.Contains(err.Error(), "Job not found") {
			resp.WriteHeader(200)
			resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
		} else {
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed stopping schedule"}`)))
		}
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
	return
}

func deleteSchedule(ctx context.Context, id string) error {
	log.Printf("Should stop schedule %s!", id)
	err := DeleteKey(ctx, "schedules", id)
	if err != nil {
		log.Printf("Failed to delete schedule: %s", err)
		return err
	} else {
		if value, exists := scheduledJobs[id]; exists {
			log.Printf("STOPPING THIS SCHEDULE: %s", id)
			// Looks like this does the trick? Hurr
			value.Lock()
		} else {
			// FIXME - allow it to kind of stop anyway?
			return errors.New("Can't find the schedule.")
		}
	}

	return nil
}

func deleteScheduleGCP(ctx context.Context, id string) error {
	c, err := scheduler.NewCloudSchedulerClient(ctx)
	if err != nil {
		log.Printf("%s", err)
		return err
	}

	req := &schedulerpb.DeleteJobRequest{
		Name: fmt.Sprintf("projects/%s/locations/europe-west2/jobs/schedule_%s", gceProject, id),
	}

	err = c.DeleteJob(ctx, req)
	if err != nil {
		log.Printf("%s", err)
		return err
	}

	return nil
}

func scheduleWorkflow(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in schedule workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID to start schedule is not valid"}`))
		return
	}

	ctx := context.Background()
	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (schedule workflow): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - have a check for org etc too..
	// FIXME - admin check like this? idk
	if user.Id != workflow.Owner && user.Role != "admin" && user.Role != "scheduler" {
		log.Printf("Wrong user (%s) for workflow %s", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(workflow.Actions) == 0 {
		workflow.Actions = []Action{}
	}
	if len(workflow.Branches) == 0 {
		workflow.Branches = []Branch{}
	}
	if len(workflow.Triggers) == 0 {
		workflow.Triggers = []Trigger{}
	}
	if len(workflow.Errors) == 0 {
		workflow.Errors = []string{}
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Failed hook unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	var schedule Schedule
	err = json.Unmarshal(body, &schedule)
	if err != nil {
		log.Printf("Failed schedule POST unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Finds the startnode for the specific schedule
	startNode := ""
	for _, branch := range workflow.Branches {
		if branch.SourceID == schedule.Id {
			startNode = branch.DestinationID
		}
	}

	if startNode == "" {
		startNode = workflow.Start
	}

	log.Printf("Startnode: %s", startNode)

	if len(schedule.Id) != 36 {
		log.Printf("ID length is not 36 for schedule: %s", err)
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(`{"success": false, "reason": "Invalid data"}`))
		return
	}

	if len(schedule.Name) == 0 {
		log.Printf("Empty name.")
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Schedule name can't be empty"}`))
		return
	}

	if len(schedule.Frequency) == 0 {
		log.Printf("Empty frequency.")
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Frequency can't be empty"}`))
		return
	}

	scheduleArg, err := json.Marshal(schedule.ExecutionArgument)
	if err != nil {
		log.Printf("Failed scheduleArg marshal: %s", err)
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Clean up garbage. This might be wrong in some very specific use-cases
	parsedBody := string(scheduleArg)
	parsedBody = strings.Replace(parsedBody, "\\\"", "\"", -1)
	if len(parsedBody) > 0 {
		if string(parsedBody[0]) == `"` && string(parsedBody[len(parsedBody)-1]) == "\"" {
			parsedBody = parsedBody[1 : len(parsedBody)-1]
		}
	}

	log.Printf("Schedulearg: %s", parsedBody)

	err = createSchedule(
		ctx,
		schedule.Id,
		workflow.ID,
		schedule.Name,
		startNode,
		schedule.Frequency,
		[]byte(parsedBody),
	)

	// FIXME - real error message lol
	if err != nil {
		log.Printf("Failed creating schedule: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Invalid argument. Try cron */15 * * * *"}`)))
		return
	}

	workflow.Schedules = append(workflow.Schedules, schedule)
	err = setWorkflow(ctx, *workflow, workflow.ID)
	if err != nil {
		log.Printf("Failed setting workflow for schedule: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
	return
}

// FIXME - add to actual database etc
func getSpecificWorkflow(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in getting specific workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if strings.Contains(fileId, "?") {
		fileId = strings.Split(fileId, "?")[0]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID when getting workflow is not valid"}`))
		return
	}

	ctx := context.Background()
	//memcacheName := fmt.Sprintf("%s_%s", user.Username, fileId)
	//if item, err := memcache.Get(ctx, memcacheName); err == memcache.ErrCacheMiss {
	//	// Not in cache
	//	log.Printf("User %s not in cache.", memcacheName)
	//} else if err != nil {
	//	log.Printf("Error getting item: %v", err)
	//} else {
	//	log.Printf("Got workflow %s from cache", fileId)
	//	// FIXME - verify if value is ok? Can unmarshal etc.
	//	resp.WriteHeader(200)
	//	resp.Write(item.Value)
	//	return
	//}

	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Workflow %s doesn't exist.", fileId)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Item already exists."}`))
		return
	}

	// CHECK orgs of user, or if user is owner
	// FIXME - add org check too, and not just owner
	// Check workflow.Sharing == private / public / org  too
	if user.Id != workflow.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s (get workflow)", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(workflow.Actions) == 0 {
		workflow.Actions = []Action{}
	}
	if len(workflow.Branches) == 0 {
		workflow.Branches = []Branch{}
	}
	if len(workflow.Triggers) == 0 {
		workflow.Triggers = []Trigger{}
	}
	if len(workflow.Errors) == 0 {
		workflow.Errors = []string{}
	}

	// Only required for individuals I think
	//newactions := []Action{}
	//for _, item := range workflow.Actions {
	//	item.LargeImage = ""
	//	item.SmallImage = ""
	//	newactions = append(newactions, item)
	//}
	//workflow.Actions = newactions

	//newtriggers := []Trigger{}
	//for _, item := range workflow.Triggers {
	//	item.LargeImage = ""
	//	newtriggers = append(newtriggers, item)
	//}
	//workflow.Triggers = newtriggers

	body, err := json.Marshal(workflow)
	if err != nil {
		log.Printf("Failed workflow GET marshalling: %s", err)
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//item := &memcache.Item{
	//	Key:        memcacheName,
	//	Value:      body,
	//	Expiration: time.Minute * 60,
	//}
	//if err := memcache.Add(ctx, item); err == memcache.ErrNotStored {
	//	if err := memcache.Set(ctx, item); err != nil {
	//		log.Printf("Error setting item: %v", err)
	//	}
	//} else if err != nil {
	//	log.Printf("error adding item: %v", err)
	//} else {
	//	//log.Printf("Set cache for %s", item.Key)
	//}

	resp.WriteHeader(200)
	resp.Write(body)
}

func setWorkflowExecution(ctx context.Context, workflowExecution WorkflowExecution) error {
	if len(workflowExecution.ExecutionId) == 0 {
		log.Printf("Workflowexeciton executionId can't be empty.")
		return errors.New("ExecutionId can't be empty.")
	}

	key := datastore.NameKey("workflowexecution", workflowExecution.ExecutionId, nil)

	// New struct, to not add body, author etc
	if _, err := dbclient.Put(ctx, key, &workflowExecution); err != nil {
		log.Printf("Error adding workflow_execution: %s", err)
		return err
	}

	return nil
}

func getWorkflowExecution(ctx context.Context, id string) (*WorkflowExecution, error) {
	key := datastore.NameKey("workflowexecution", strings.ToLower(id), nil)
	workflowExecution := &WorkflowExecution{}
	if err := dbclient.Get(ctx, key, workflowExecution); err != nil {
		return &WorkflowExecution{}, err
	}

	return workflowExecution, nil
}

func getApp(ctx context.Context, id string) (*WorkflowApp, error) {
	key := datastore.NameKey("workflowapp", strings.ToLower(id), nil)
	workflowApp := &WorkflowApp{}
	if err := dbclient.Get(ctx, key, workflowApp); err != nil {
		return &WorkflowApp{}, err
	}

	return workflowApp, nil
}

func getWorkflow(ctx context.Context, id string) (*Workflow, error) {
	key := datastore.NameKey("workflow", strings.ToLower(id), nil)
	workflow := &Workflow{}
	if err := dbclient.Get(ctx, key, workflow); err != nil {
		return &Workflow{}, err
	}

	return workflow, nil
}

func getEnvironments(ctx context.Context) ([]Environment, error) {
	var environments []Environment
	q := datastore.NewQuery("Environments")

	_, err := dbclient.GetAll(ctx, q, &environments)
	if err != nil {
		return []Environment{}, err
	}

	return environments, nil
}

func getAllWorkflows(ctx context.Context) ([]Workflow, error) {
	var allworkflows []Workflow
	q := datastore.NewQuery("workflow")

	_, err := dbclient.GetAll(ctx, q, &allworkflows)
	if err != nil {
		return []Workflow{}, err
	}

	return allworkflows, nil
}

// Hmm, so I guess this should use uuid :(
// Consistency PLX
func setWorkflow(ctx context.Context, workflow Workflow, id string) error {
	key := datastore.NameKey("workflow", id, nil)

	// New struct, to not add body, author etc
	if _, err := dbclient.Put(ctx, key, &workflow); err != nil {
		log.Printf("Error adding workflow: %s", err)
		return err
	}

	return nil
}

func deleteAppAuthentication(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in edit workflow: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Role != "admin" {
		log.Printf("Need to be admin to delete appauth")
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")
	log.Printf("%#v", location)
	var fileId string
	if location[1] == "api" {
		if len(location) <= 5 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[5]
	}

	// FIXME: Set affected workflows to have errors
	// 1. Get the auth
	// 2. Loop the workflows (.Usage) and set them to have errors
	// 3. Loop the nodes in workflows and do the same

	log.Printf("ID: %s", fileId)
	ctx := context.Background()
	err := DeleteKey(ctx, "workflowappauth", fileId)
	if err != nil {
		log.Printf("Failed deleting workflowapp")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed deleting workflow app"}`)))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true}`))
}

func deleteWorkflowApp(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in edit workflow: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")
	log.Printf("%#v", location)
	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	ctx := context.Background()
	log.Printf("ID: %s", fileId)
	app, err := getApp(ctx, fileId)
	if err != nil {
		log.Printf("Error getting app %s: %s", app.Name, err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - check whether it's in use and maybe restrict again for later?
	// FIXME - actually delete other than private apps too..
	private := false
	if app.Downloaded {
		log.Printf("Deleting downloaded app (authenticated users can do this)")
	} else if user.Id != app.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for app %s (delete)", user.Username, app.Name)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	} else {
		private = true
	}

	q := datastore.NewQuery("workflow")
	var workflows []Workflow
	_, err = dbclient.GetAll(ctx, q, &workflows)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "}`))
		return
	}

	for _, workflow := range workflows {
		found := false

		newActions := []Action{}
		for _, action := range workflow.Actions {
			if action.AppName == app.Name && action.AppVersion == app.AppVersion {
				found = true
				action.Errors = append(action.Errors, "App has been deleted")
				action.IsValid = false
			}

			newActions = append(newActions, action)
		}

		if found {
			workflow.IsValid = false
			workflow.Errors = append(workflow.Errors, fmt.Sprintf("App %s_%s has been deleted", app.Name, app.AppVersion))
			workflow.Actions = newActions

			for _, trigger := range workflow.Triggers {
				log.Printf("TRIGGER: %#v", trigger)
				//err = deleteSchedule(ctx, scheduleId)
				//if err != nil {
				//	if strings.Contains(err.Error(), "Job not found") {
				//		resp.WriteHeader(200)
				//		resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
				//	} else {
				//		resp.WriteHeader(401)
				//		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed stopping schedule"}`)))
				//	}
				//	return
				//}
			}

			err = setWorkflow(ctx, workflow, workflow.ID)
			if err != nil {
				log.Printf("Failed setting workflow when deleting app: %s", err)
				continue
			} else {
				log.Printf("Set %s (%s) to have errors", workflow.ID, workflow.Name)
			}

		}

	}

	//resp.WriteHeader(200)
	//resp.Write([]byte(`{"success": true}`))
	//return

	// Not really deleting it, just removing from user cache
	if private {
		log.Printf("Deleting private app")
		var privateApps []WorkflowApp
		for _, item := range user.PrivateApps {
			if item.ID == fileId {
				continue
			}

			privateApps = append(privateApps, item)
		}

		user.PrivateApps = privateApps
		err = setUser(ctx, &user)
		if err != nil {
			log.Printf("Failed removing %s app for user %s: %s", app.Name, user.Username, err)
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": true"}`)))
			return
		}
	}

	log.Printf("Deleting public app")
	err = DeleteKey(ctx, "workflowapp", fileId)
	if err != nil {
		log.Printf("Failed deleting workflowapp")
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed deleting workflow app"}`)))
		return
	}

	err = increaseStatisticsField(ctx, "total_apps_deleted", fileId, 1)
	if err != nil {
		log.Printf("Failed to increase total apps loaded stats: %s", err)
	}
	//err = memcache.Delete(request.Context(), sessionToken)
	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true}`))
}

func getWorkflowAppConfig(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in edit workflow: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")
	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	ctx := context.Background()
	app, err := getApp(ctx, fileId)
	if err != nil {
		log.Printf("Error getting app: %s", app.Name)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Id != app.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for app %s", user.Username, app.Name)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Printf("Getting app %s", fileId)
	parsedApi, err := getOpenApiDatastore(ctx, fileId)
	if err != nil {
		log.Printf("OpenApi doesn't exist for: %s - err: %s", fileId, err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//log.Printf("Parsed API: %#v", parsedApi)
	if len(parsedApi.ID) > 0 {
		parsedApi.Success = true
	} else {
		parsedApi.Success = false
	}

	data, err := json.Marshal(parsedApi)
	if err != nil {
		resp.WriteHeader(422)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed marshalling new parsed swagger: %s"}`, err)))
		return
	}

	resp.WriteHeader(200)
	resp.Write(data)
}

func addAppAuthentication(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// FIXME - need to be logged in?
	_, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in get all apps: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	var appAuth AppAuthenticationStorage
	err = json.Unmarshal(body, &appAuth)
	if err != nil {
		log.Printf("Failed unmarshaling (appauth): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(appAuth.Id) == 0 {
		appAuth.Id = uuid.NewV4().String()
	}

	ctx := context.Background()
	if len(appAuth.Label) == 0 {
		resp.WriteHeader(409)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Label can't be empty"}`)))
		return
	}

	// Super basic check
	if len(appAuth.App.ID) != 36 && len(appAuth.App.ID) != 32 {
		log.Printf("Bad ID for app: %s", appAuth.App.ID)
		resp.WriteHeader(409)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "App has to be defined"}`)))
		return
	}

	app, err := getApp(ctx, appAuth.App.ID)
	if err != nil {
		log.Printf("Failed finding app %s while setting auth.", appAuth.App.ID)
		resp.WriteHeader(409)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	// Check if the items are correct
	for _, field := range appAuth.Fields {
		found := false
		for _, param := range app.Authentication.Parameters {
			if field.Key == param.Name {
				found = true
			}
		}

		if !found {
			log.Printf("Failed finding field %s in appauth fields", field.Key)
			resp.WriteHeader(409)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "All auth fields required"}`)))
			return
		}
	}

	err = setWorkflowAppAuthDatastore(ctx, appAuth, appAuth.Id)
	if err != nil {
		log.Printf("Failed setting up app auth %s: %s", appAuth.Id, err)
		resp.WriteHeader(409)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true}`))
}

func getAppAuthentication(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	_, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in get all apps: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME: Auth to get the right ones only
	//if user.Role != "admin" {
	//	log.Printf("User isn't admin")
	//	resp.WriteHeader(401)
	//	resp.Write([]byte(`{"success": false}`))
	//	return
	//}
	ctx := context.Background()
	allAuths, err := getAllWorkflowAppAuth(ctx)
	if err != nil {
		log.Printf("Api authentication failed in get all app auth: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(allAuths) == 0 {
		resp.WriteHeader(200)
		resp.Write([]byte(`{"success": true, "data": []}`))
		return
	}

	// Cleanup for frontend
	newAuth := []AppAuthenticationStorage{}
	for _, auth := range allAuths {
		newAuthField := auth
		for index, _ := range auth.Fields {
			newAuthField.Fields[index].Value = "auth placeholder (replaced during execution)"
		}

		newAuth = append(newAuth, newAuthField)
	}

	newbody, err := json.Marshal(allAuths)
	if err != nil {
		log.Printf("Failed unmarshalling all app auths: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow app auth"}`)))
		return
	}

	data := fmt.Sprintf(`{"success": true, "data": %s}`, string(newbody))

	resp.WriteHeader(200)
	resp.Write([]byte(data))

	/*
		data := `{
			"success": true,
			"data": [
				{
					"app": {
						"name": "thehive",
						"description": "what",
						"app_version": "1.0.0",
						"id": "4f97da9d-1caf-41cc-aa13-67104d8d825c",
						"large_image": "asd"
					},
					"fields": {
						"apikey": "hello",
						"url": "url"
					},
					"usage": [{
						"workflow_id": "asd",
						"nodes": [{
							"node_id": ""
						}]
					}],
					"label": "Original",
					"id": "4f97da9d-1caf-41cc-aa13-67104d8d825d",
					"active": true
				},
				{
					"app": {
						"name": "thehive",
						"description": "what",
						"app_version": "1.0.0",
						"id": "4f97da9d-1caf-41cc-aa13-67104d8d825c",
						"large_image": "asd"
					},
					"fields": {
						"apikey": "hello",
						"url": "url"
					},
					"usage": [{
						"workflow_id": "asd",
						"nodes": [{
							"node_id": ""
						}]
					}],
					"label": "Number 2",
					"id": "4f97da9d-1caf-41cc-aa13-67104d8d825d",
					"active": true
				}
			]
		}`
	*/
}
func updateWorkflowAppConfig(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in get all apps: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")
	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	ctx := context.Background()
	app, err := getApp(ctx, fileId)
	if err != nil {
		log.Printf("Error getting app: %s (update app)", app.Name)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Id != app.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for app %s in update app", user.Username, app.Name)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read in update app: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	type updatefields struct {
		Sharing       bool   `json:"sharing"`
		SharingConfig string `json:"sharing_config"`
	}

	var tmpfields updatefields
	err = json.Unmarshal(body, &tmpfields)
	if err != nil {
		log.Printf("Error with unmarshal body in update app: %s\n%s", err, string(body))
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if tmpfields.Sharing != app.Sharing {
		app.Sharing = tmpfields.Sharing
	}

	if tmpfields.SharingConfig != app.SharingConfig {
		app.SharingConfig = tmpfields.SharingConfig
	}

	err = setWorkflowAppDatastore(ctx, *app, app.ID)
	if err != nil {
		log.Printf("Failed patching workflowapp: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Printf("Changed workflow app %s", app.ID)
	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

func getWorkflowApps(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// FIXME - set this to be per user IF logged in, as there might exist private and public
	//memcacheName := "all_apps"

	ctx := context.Background()
	// Just need to be logged in
	// FIXME - need to be logged in?
	user, userErr := handleApiAuthentication(resp, request)
	if userErr != nil {
		log.Printf("Api authentication failed in get all apps: %s", userErr)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//if item, err := memcache.Get(ctx, memcacheName); err == memcache.ErrCacheMiss {
	//	// Not in cache
	//	log.Printf("Apps not in cache.")
	//} else if err != nil {
	//	log.Printf("Error getting item: %v", err)
	//} else {
	//	// FIXME - verify if value is ok? Can unmarshal etc.
	//	allApps := item.Value

	//	if userErr == nil && len(user.PrivateApps) > 0 {
	//		var parsedApps []WorkflowApp
	//		err = json.Unmarshal(allApps, &parsedApps)
	//		if err == nil {
	//			log.Printf("Shouldve added %d apps", len(user.PrivateApps))
	//			user.PrivateApps = append(user.PrivateApps, parsedApps...)

	//			tmpApps, err := json.Marshal(user.PrivateApps)
	//			if err == nil {
	//				allApps = tmpApps
	//			}
	//		}
	//	}

	//	resp.WriteHeader(200)
	//	resp.Write(allApps)
	//	return
	//}

	workflowapps, err := getAllWorkflowApps(ctx)
	if err != nil {
		log.Printf("Failed getting apps: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}
	//log.Printf("Length: %d", len(workflowapps))

	// FIXME - this is really garbage, but is here to protect again null values etc.
	newapps := []WorkflowApp{}
	baseApps := []WorkflowApp{}

	for _, workflowapp := range workflowapps {
		if !workflowapp.Activated && workflowapp.Generated {
			continue
		}

		if workflowapp.Owner != user.Id && user.Role != "admin" && !workflowapp.Sharing {
			continue
		}

		//workflowapp.Environment = "cloud"
		newactions := []WorkflowAppAction{}
		for _, action := range workflowapp.Actions {
			//action.Environment = workflowapp.Environment
			if len(action.Parameters) == 0 {
				action.Parameters = []WorkflowAppActionParameter{}
			}

			newactions = append(newactions, action)
		}

		workflowapp.Actions = newactions
		newapps = append(newapps, workflowapp)
		baseApps = append(baseApps, workflowapp)
	}

	if len(user.PrivateApps) > 0 {
		found := false
		for _, item := range user.PrivateApps {
			for _, app := range newapps {
				if item.ID == app.ID {
					found = true
					break
				}
			}

			if !found {
				newapps = append(newapps, item)
			}
		}
	}

	// Double unmarshal because of user apps
	newbody, err := json.Marshal(newapps)
	//newbody, err := json.Marshal(workflowapps)
	if err != nil {
		log.Printf("Failed unmarshalling all newapps: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow apps"}`)))
		return
	}

	//basebody, err := json.Marshal(baseApps)
	////newbody, err := json.Marshal(workflowapps)
	//if err != nil {
	//	log.Printf("Failed unmarshalling all baseapps: %s", err)
	//	resp.WriteHeader(401)
	//	resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow apps"}`)))
	//	return
	//}

	// Refreshed every hour
	//item := &memcache.Item{
	//	Key:        memcacheName,
	//	Value:      basebody,
	//	Expiration: time.Minute * 60,
	//}
	//if err := memcache.Add(ctx, item); err == memcache.ErrNotStored {
	//	if err := memcache.Set(ctx, item); err != nil {
	//		log.Printf("Error setting item: %v", err)
	//	}
	//} else if err != nil {
	//	log.Printf("error adding item: %v", err)
	//} else {
	//	log.Printf("Set cache for %s", item.Key)
	//}

	//log.Println(string(body))
	//log.Println(string(newbody))
	resp.WriteHeader(200)
	resp.Write(newbody)
}

// Bad check for workflowapps :)
// FIXME - use tags and struct reflection
func checkWorkflowApp(workflowApp WorkflowApp) error {
	// Validate fields
	if workflowApp.Name == "" {
		return errors.New("App field name doesn't exist")
	}

	if workflowApp.Description == "" {
		return errors.New("App field description doesn't exist")
	}

	if workflowApp.AppVersion == "" {
		return errors.New("App field app_version doesn't exist")
	}

	if workflowApp.ContactInfo.Name == "" {
		return errors.New("App field contact_info.name doesn't exist")
	}

	return nil
}

func handleGetfile(resp http.ResponseWriter, request *http.Request) ([]byte, error) {
	// Upload file here first
	request.ParseMultipartForm(32 << 20)
	file, _, err := request.FormFile("file")
	if err != nil {
		log.Printf("Error parsing: %s", err)
		return []byte{}, err
	}
	defer file.Close()

	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, file); err != nil {
		return []byte{}, err
	}

	return buf.Bytes(), nil
}

// Basically a search for apps that aren't activated yet
func getSpecificApps(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// Just need to be logged in
	// FIXME - should have some permissions?
	_, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new app: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	type tmpStruct struct {
		Search string `json:"search"`
	}

	var tmpBody tmpStruct
	err = json.Unmarshal(body, &tmpBody)
	if err != nil {
		log.Printf("Error with unmarshal tmpBody: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - continue the search here with github repos etc.
	// Caching might be smart :D
	ctx := context.Background()
	workflowapps, err := getAllWorkflowApps(ctx)
	if err != nil {
		log.Printf("Error: Failed getting workflowapps: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	returnValues := []WorkflowApp{}
	search := strings.ToLower(tmpBody.Search)
	for _, app := range workflowapps {
		if !app.Activated && app.Generated {
			// This might be heavy with A LOT
			// Not too worried with todays tech tbh..
			appName := strings.ToLower(app.Name)
			appDesc := strings.ToLower(app.Description)
			if strings.Contains(appName, search) || strings.Contains(appDesc, search) {
				//log.Printf("Name: %s, Generated: %s, Activated: %s", app.Name, strconv.FormatBool(app.Generated), strconv.FormatBool(app.Activated))
				returnValues = append(returnValues, app)
			}
		}
	}

	newbody, err := json.Marshal(returnValues)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow executions"}`)))
		return
	}

	returnData := fmt.Sprintf(`{"success": true, "reason": %s}`, string(newbody))
	resp.WriteHeader(200)
	resp.Write([]byte(returnData))
}

func validateAppInput(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// Just need to be logged in
	// FIXME - should have some permissions?
	_, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new app: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	filebytes, err := handleGetfile(resp, request)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	kind, err := filetype.Match(filebytes)
	if err != nil {
		log.Printf("Failed parsing filetype")
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	//fmt.Printf("File type: %s. MIME: %s\n", kind.Extension, kind.MIME.Value)
	if kind == filetype.Unknown {
		fmt.Println("Unknown file type")
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if kind.MIME.Value != "application/zip" {
		fmt.Println("Not zip, can't unzip")
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - validate folderstructure, Dockerfile, python scripts, api.yaml, requirements.txt, src/

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

// Deploy to google cloud function :)
func deployCloudFunctionPython(ctx context.Context, name, localization, applocation string, environmentVariables map[string]string) error {
	service, err := cloudfunctions.NewService(ctx)
	if err != nil {
		return err
	}

	// ProjectsLocationsListCall
	projectsLocationsFunctionsService := cloudfunctions.NewProjectsLocationsFunctionsService(service)
	location := fmt.Sprintf("projects/%s/locations/%s", gceProject, localization)
	functionName := fmt.Sprintf("%s/functions/%s", location, name)

	cloudFunction := &cloudfunctions.CloudFunction{
		AvailableMemoryMb:    128,
		EntryPoint:           "authorization",
		EnvironmentVariables: environmentVariables,
		HttpsTrigger:         &cloudfunctions.HttpsTrigger{},
		MaxInstances:         0,
		Name:                 functionName,
		Runtime:              "python37",
		SourceArchiveUrl:     applocation,
	}

	//getCall := projectsLocationsFunctionsService.Get(fmt.Sprintf("%s/functions/function-5", location))
	//resp, err := getCall.Do()

	createCall := projectsLocationsFunctionsService.Create(location, cloudFunction)
	_, err = createCall.Do()
	if err != nil {
		log.Printf("Failed creating new function. SKIPPING patch, as it probably already exists: %s", err)

		// FIXME - have patching code or nah?
		createCall := projectsLocationsFunctionsService.Patch(fmt.Sprintf("%s/functions/%s", location, name), cloudFunction)
		_, err = createCall.Do()
		if err != nil {
			log.Println("Failed patching function")
			return err
		}

		log.Printf("Successfully patched %s to %s", name, localization)
	} else {
		log.Printf("Successfully deployed %s to %s", name, localization)
	}

	// FIXME - use response to define the HTTPS entrypoint. It's default to an easy one tho

	return nil
}

// Deploy to google cloud function :)
func deployCloudFunctionGo(ctx context.Context, name, localization, applocation string, environmentVariables map[string]string) error {
	service, err := cloudfunctions.NewService(ctx)
	if err != nil {
		return err
	}

	// ProjectsLocationsListCall
	projectsLocationsFunctionsService := cloudfunctions.NewProjectsLocationsFunctionsService(service)
	location := fmt.Sprintf("projects/%s/locations/%s", gceProject, localization)
	functionName := fmt.Sprintf("%s/functions/%s", location, name)

	cloudFunction := &cloudfunctions.CloudFunction{
		AvailableMemoryMb:    128,
		EntryPoint:           "Authorization",
		EnvironmentVariables: environmentVariables,
		HttpsTrigger:         &cloudfunctions.HttpsTrigger{},
		MaxInstances:         1,
		Name:                 functionName,
		Runtime:              "go111",
		SourceArchiveUrl:     applocation,
	}

	//getCall := projectsLocationsFunctionsService.Get(fmt.Sprintf("%s/functions/function-5", location))
	//resp, err := getCall.Do()

	createCall := projectsLocationsFunctionsService.Create(location, cloudFunction)
	_, err = createCall.Do()
	if err != nil {
		log.Println("Failed creating new function. Attempting patch, as it might exist already")

		createCall := projectsLocationsFunctionsService.Patch(fmt.Sprintf("%s/functions/%s", location, name), cloudFunction)
		_, err = createCall.Do()
		if err != nil {
			log.Println("Failed patching function")
			return err
		}

		log.Printf("Successfully patched %s to %s", name, localization)
	} else {
		log.Printf("Successfully deployed %s to %s", name, localization)
	}

	// FIXME - use response to define the HTTPS entrypoint. It's default to an easy one tho

	return nil
}

// Deploy to google cloud function :)
func deployWebhookFunction(ctx context.Context, name, localization, applocation string, environmentVariables map[string]string) error {
	service, err := cloudfunctions.NewService(ctx)
	if err != nil {
		return err
	}

	// ProjectsLocationsListCall
	projectsLocationsFunctionsService := cloudfunctions.NewProjectsLocationsFunctionsService(service)
	location := fmt.Sprintf("projects/%s/locations/%s", gceProject, localization)
	functionName := fmt.Sprintf("%s/functions/%s", location, name)

	cloudFunction := &cloudfunctions.CloudFunction{
		AvailableMemoryMb:    128,
		EntryPoint:           "Authorization",
		EnvironmentVariables: environmentVariables,
		HttpsTrigger:         &cloudfunctions.HttpsTrigger{},
		MaxInstances:         1,
		Name:                 functionName,
		Runtime:              "go111",
		SourceArchiveUrl:     applocation,
	}

	//getCall := projectsLocationsFunctionsService.Get(fmt.Sprintf("%s/functions/function-5", location))
	//resp, err := getCall.Do()

	createCall := projectsLocationsFunctionsService.Create(location, cloudFunction)
	_, err = createCall.Do()
	if err != nil {
		log.Println("Failed creating new function. Attempting patch, as it might exist already")

		createCall := projectsLocationsFunctionsService.Patch(fmt.Sprintf("%s/functions/%s", location, name), cloudFunction)
		_, err = createCall.Do()
		if err != nil {
			log.Println("Failed patching function")
			return err
		}

		log.Printf("Successfully patched %s to %s", name, localization)
	} else {
		log.Printf("Successfully deployed %s to %s", name, localization)
	}

	// FIXME - use response to define the HTTPS entrypoint. It's default to an easy one tho

	return nil
}

func loadGithubWorkflows(url, username, password, userId, branch string) error {
	fs := memfs.New()

	if strings.Contains(url, "github") || strings.Contains(url, "gitlab") || strings.Contains(url, "bitbucket") {
		cloneOptions := &git.CloneOptions{
			URL: url,
		}

		// FIXME: Better auth.
		if len(username) > 0 && len(password) > 0 {
			cloneOptions.Auth = &http2.BasicAuth{

				Username: username,
				Password: password,
			}
		}

		if len(branch) > 0 {
			cloneOptions.ReferenceName = plumbing.ReferenceName(branch)
		}

		storer := memory.NewStorage()
		r, err := git.Clone(storer, fs, cloneOptions)
		if err != nil {
			log.Printf("Failed loading repo into memory: %s", err)
			return err
		}

		dir, err := fs.ReadDir("/")
		if err != nil {
			log.Printf("FAiled reading folder: %s", err)
		}
		_ = r

		log.Printf("Starting workflow folder iteration")
		iterateWorkflowGithubFolders(fs, dir, "", "", userId)

	} else if strings.Contains(url, "s3") {
		//https://docs.aws.amazon.com/sdk-for-go/api/service/s3/

		//sess := session.Must(session.NewSession())
		//downloader := s3manager.NewDownloader(sess)

		//// Write the contents of S3 Object to the file
		//storer := memory.NewStorage()
		//n, err := downloader.Download(storer, &s3.GetObjectInput{
		//	Bucket: aws.String(myBucket),
		//	Key:    aws.String(myString),
		//})
		//if err != nil {
		//	return fmt.Errorf("failed to download file, %v", err)
		//}
		//fmt.Printf("file downloaded, %d bytes\n", n)
	} else {
		return errors.New(fmt.Sprintf("URL %s is unsupported when downloading workflows", url))
	}

	return nil
}

func loadSpecificWorkflows(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// Just need to be logged in
	// FIXME - should have some permissions?
	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in load apps: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Role != "admin" {
		log.Printf("Wrong user (%s) when downloading from github", user.Username)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Downloading remotely requires admin"}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Field1 & 2 can be a lot of things..
	type tmpStruct struct {
		URL    string `json:"url"`
		Field1 string `json:"field_1"`
		Field2 string `json:"field_2"`
		Field3 string `json:"field_3"`
	}
	//log.Printf("Body: %s", string(body))

	var tmpBody tmpStruct
	err = json.Unmarshal(body, &tmpBody)
	if err != nil {
		log.Printf("Error with unmarshal tmpBody: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Field3 = branch
	err = loadGithubWorkflows(tmpBody.URL, tmpBody.Field1, tmpBody.Field2, user.Id, tmpBody.Field3)
	if err != nil {
		log.Printf("Failed to update workflows: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

func handleAppHotloadRequest(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	log.Printf("Starting app hotloading")

	// Just need to be logged in
	// FIXME - should have some permissions?
	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in app hotload: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Role != "admin" {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Must be admin to hotload apps"}`))
		return
	}

	location := os.Getenv("SHUFFLE_APP_HOTLOAD_FOLDER")
	if len(location) == 0 {
		resp.WriteHeader(500)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "SHUFFLE_APP_HOTLOAD_FOLDER not specified in .env"}`)))
		return
	}

	log.Printf("Hotloading from %s", location)
	err = handleAppHotload(location, true)
	if err != nil {
		resp.WriteHeader(500)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed loading apps"}`)))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

func loadSpecificApps(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// Just need to be logged in
	// FIXME - should have some permissions?
	_, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in load specific apps: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Field1 & 2 can be a lot of things..
	type tmpStruct struct {
		URL         string `json:"url"`
		Field1      string `json:"field_1"`
		Field2      string `json:"field_2"`
		ForceUpdate bool   `json:"force_update"`
	}
	//log.Printf("Body: %s", string(body))

	var tmpBody tmpStruct
	err = json.Unmarshal(body, &tmpBody)
	if err != nil {
		log.Printf("Error with unmarshal tmpBody: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	fs := memfs.New()

	if strings.Contains(tmpBody.URL, "github") || strings.Contains(tmpBody.URL, "gitlab") || strings.Contains(tmpBody.URL, "bitbucket") {
		cloneOptions := &git.CloneOptions{
			URL: tmpBody.URL,
		}

		// FIXME: Better auth.
		if len(tmpBody.Field1) > 0 && len(tmpBody.Field2) > 0 {
			cloneOptions.Auth = &http2.BasicAuth{
				Username: tmpBody.Field1,
				Password: tmpBody.Field2,
			}
		}

		storer := memory.NewStorage()
		r, err := git.Clone(storer, fs, cloneOptions)
		if err != nil {
			log.Printf("Failed loading repo into memory: %s", err)
			resp.WriteHeader(401)
			resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
			return
		}

		dir, err := fs.ReadDir("/")
		if err != nil {
			log.Printf("FAiled reading folder: %s", err)
		}
		_ = r

		if tmpBody.ForceUpdate {
			log.Printf("Running with force update!")
		} else {
			log.Printf("Updating apps with updates")
		}
		iterateAppGithubFolders(fs, dir, "", "", tmpBody.ForceUpdate)

	} else if strings.Contains(tmpBody.URL, "s3") {
		//https://docs.aws.amazon.com/sdk-for-go/api/service/s3/

		//sess := session.Must(session.NewSession())
		//downloader := s3manager.NewDownloader(sess)

		//// Write the contents of S3 Object to the file
		//storer := memory.NewStorage()
		//n, err := downloader.Download(storer, &s3.GetObjectInput{
		//	Bucket: aws.String(myBucket),
		//	Key:    aws.String(myString),
		//})
		//if err != nil {
		//	return fmt.Errorf("failed to download file, %v", err)
		//}
		//fmt.Printf("file downloaded, %d bytes\n", n)
	} else {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s is unsupported"}`, tmpBody.URL)))
		return
	}

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

func iterateOpenApiGithub(fs billy.Filesystem, dir []os.FileInfo, extra string, onlyname string) error {

	ctx := context.Background()
	workflowapps, err := getAllWorkflowApps(ctx)
	appCounter := 0
	if err != nil {
		log.Printf("Failed to get existing generated apps")
	}
	for _, file := range dir {
		if len(onlyname) > 0 && file.Name() != onlyname {
			continue
		}

		// Folder?
		switch mode := file.Mode(); {
		case mode.IsDir():
			tmpExtra := fmt.Sprintf("%s%s/", extra, file.Name())
			//log.Printf("TMPEXTRA: %s", tmpExtra)
			dir, err := fs.ReadDir(tmpExtra)
			if err != nil {
				log.Printf("Failed reading dir in openapi: %s", err)
				continue
			}

			// Go routine? Hmm, this can be super quick I guess
			err = iterateOpenApiGithub(fs, dir, tmpExtra, "")
			if err != nil {
				log.Printf("Failed recursion in openapi: %s", err)
				continue
				//break
			}
		case mode.IsRegular():
			// Check the file
			filename := file.Name()
			if strings.Contains(filename, "yaml") || strings.Contains(filename, "yml") {
				//log.Printf("File: %s", filename)
				//log.Printf("Found file: %s", filename)
				log.Printf("OpenAPI app: %s", filename)
				tmpExtra := fmt.Sprintf("%s%s/", extra, file.Name())

				fileReader, err := fs.Open(tmpExtra)
				if err != nil {
					continue
				}

				readFile, err := ioutil.ReadAll(fileReader)
				if err != nil {
					log.Printf("Filereader error yaml for %s: %s", filename, err)
					continue
				}

				// 1. This parses OpenAPI v2 to v3 etc, for use.
				parsedOpenApi, err := handleSwaggerValidation(readFile)
				if err != nil {
					log.Printf("Validation error for %s: %s", filename, err)
					continue
				}

				// 2. With parsedOpenApi.ID:
				//http://localhost:3000/apps/new?id=06b1376f77b0563a3b1747a3a1253e88

				// 3. Load this as a "standby" app
				// FIXME: This should be a function ROFL
				//log.Printf("%s", string(readFile))
				swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromData([]byte(parsedOpenApi.Body))
				if err != nil {
					log.Printf("Swagger validation error in loop (%s): %s", filename, err)
					continue
				}

				if strings.Contains(swagger.Info.Title, " ") {
					strings.Replace(swagger.Info.Title, " ", "", -1)
				}

				//log.Printf("Should generate yaml")
				swagger, api, _, err := generateYaml(swagger, parsedOpenApi.ID)
				if err != nil {
					log.Printf("Failed building and generating yaml in loop (%s): %s", filename, err)
					continue
				}

				// FIXME: Configure user?
				api.Owner = ""
				api.ID = parsedOpenApi.ID
				api.IsValid = true
				api.Generated = true
				api.Activated = false

				found := false
				for _, app := range workflowapps {
					if app.ID == api.ID {
						found = true
						break
					} else if app.Name == api.Name && app.AppVersion == api.AppVersion {
						found = true
						break
					}
				}

				if !found {
					err = setWorkflowAppDatastore(ctx, api, api.ID)
					if err != nil {
						log.Printf("Failed setting workflowapp in loop: %s", err)
						continue
					} else {
						appCounter += 1
						log.Printf("Added %s:%s to the database from OpenAPI repo", api.Name, api.AppVersion)

						// Set OpenAPI datastore
						err = setOpenApiDatastore(ctx, parsedOpenApi.ID, parsedOpenApi)
						if err != nil {
							log.Printf("Failed uploading openapi to datastore in loop: %s", err)
							continue
						}
					}
				} else {
					//log.Printf("Skipped upload of %s (%s)", api.Name, api.ID)
				}

				//return nil
			}
		}
	}

	if appCounter > 0 {
		log.Printf("Preloaded %d OpenApi apps in %s!", appCounter, extra)
	}

	return nil
}

// Onlyname is used to
func iterateWorkflowGithubFolders(fs billy.Filesystem, dir []os.FileInfo, extra string, onlyname string, userId string) error {
	var err error

	for _, file := range dir {
		if len(onlyname) > 0 && file.Name() != onlyname {
			continue
		}

		// Folder?
		switch mode := file.Mode(); {
		case mode.IsDir():
			tmpExtra := fmt.Sprintf("%s%s/", extra, file.Name())
			dir, err := fs.ReadDir(tmpExtra)
			if err != nil {
				log.Printf("Failed to read dir: %s", err)
				continue
			}

			// Go routine? Hmm, this can be super quick I guess
			err = iterateWorkflowGithubFolders(fs, dir, tmpExtra, "", userId)
			if err != nil {
				continue
			}
		case mode.IsRegular():
			// Check the file
			filename := file.Name()
			if strings.HasSuffix(filename, ".json") {
				path := fmt.Sprintf("%s%s", extra, file.Name())
				fileReader, err := fs.Open(path)
				if err != nil {
					log.Printf("Error reading file: %s", err)
					continue
				}

				readFile, err := ioutil.ReadAll(fileReader)
				if err != nil {
					log.Printf("Error reading file: %s", err)
					continue
				}

				var workflow Workflow
				err = json.Unmarshal(readFile, &workflow)
				if err != nil {
					continue
				}

				// rewrite owner to user who imports now
				if userId != "" {
					workflow.Owner = userId
				}

				ctx := context.Background()
				err = setWorkflow(ctx, workflow, workflow.ID)
				if err != nil {
					log.Printf("Failed setting (download) workflow: %s", err)
					continue
				}
				log.Printf("Uploaded workflow %s for user %s!", filename, userId)
			}
		}
	}

	return err
}

// Onlyname is used to
func iterateAppGithubFolders(fs billy.Filesystem, dir []os.FileInfo, extra string, onlyname string, forceUpdate bool) error {
	var err error

	allapps := []WorkflowApp{}

	// It's here to prevent getting them in every iteration
	ctx := context.Background()
	for _, file := range dir {
		if len(onlyname) > 0 && file.Name() != onlyname {
			continue
		}

		// Folder?
		switch mode := file.Mode(); {
		case mode.IsDir():
			tmpExtra := fmt.Sprintf("%s%s/", extra, file.Name())
			dir, err := fs.ReadDir(tmpExtra)
			if err != nil {
				log.Printf("Failed to read dir: %s", err)
				continue
			}

			// Go routine? Hmm, this can be super quick I guess
			err = iterateAppGithubFolders(fs, dir, tmpExtra, "", forceUpdate)
			if err != nil {
				log.Printf("Error reading folder: %s", err)
				continue
			}
		case mode.IsRegular():
			// Check the file
			filename := file.Name()
			if filename == "Dockerfile" {
				// Set up to make md5 and check if the app is new (api.yaml+src/app.py+Dockerfile)
				// Check if Dockerfile, app.py or api.yaml has changed. Hash?
				//log.Printf("Handle Dockerfile in location %s", extra)
				// Try api.yaml and api.yml
				fullPath := fmt.Sprintf("%s%s", extra, "api.yaml")
				fileReader, err := fs.Open(fullPath)
				if err != nil {
					fullPath = fmt.Sprintf("%s%s", extra, "api.yml")
					fileReader, err = fs.Open(fullPath)
					if err != nil {
						log.Printf("Failed finding api.yaml/yml: %s", err)
						continue
					}
				}

				appfileData, err := ioutil.ReadAll(fileReader)
				if err != nil {
					log.Printf("Failed reading %s: %s", fullPath, err)
					continue
				}

				if len(appfileData) == 0 {
					log.Printf("Failed reading %s - length is 0.", fullPath)
					continue
				}

				// func md5sum(data []byte) string {
				// Make hash
				appPython := fmt.Sprintf("%s/src/app.py", extra)
				appPythonReader, err := fs.Open(appPython)
				if err != nil {
					log.Printf("Failed to read %s", appPython)
					continue
				}

				appPythonData, err := ioutil.ReadAll(appPythonReader)
				if err != nil {
					log.Printf("Failed reading %s: %s", appPython, err)
					continue
				}

				dockerFp := fmt.Sprintf("%s/Dockerfile", extra)
				dockerfile, err := fs.Open(dockerFp)
				if err != nil {
					log.Printf("Failed to read %s", appPython)
					continue
				}

				dockerfileData, err := ioutil.ReadAll(dockerfile)
				if err != nil {
					log.Printf("Failed to read dockerfile")
					continue
				}

				combined := []byte{}
				combined = append(combined, appfileData...)
				combined = append(combined, appPythonData...)
				combined = append(combined, dockerfileData...)
				md5 := md5sum(combined)

				var workflowapp WorkflowApp
				err = gyaml.Unmarshal(appfileData, &workflowapp)
				if err != nil {
					log.Printf("Failed unmarshaling workflowapp %s: %s", fullPath, err)
					continue
				}

				newName := workflowapp.Name
				newName = strings.ReplaceAll(newName, " ", "-")

				tags := []string{
					fmt.Sprintf("%s:%s_%s", baseDockerName, newName, workflowapp.AppVersion),
				}

				if len(allapps) == 0 {
					allapps, err = getAllWorkflowApps(ctx)
					if err != nil {
						log.Printf("Failed getting apps to verify: %s", err)
						continue
					}
				}

				// Make an option to override existing apps?
				//Hash string `json:"hash" datastore:"hash" yaml:"hash"` // api.yaml+dockerfile+src/app.py for apps
				removeApps := []string{}
				skip := false
				for _, app := range allapps {
					if app.Name == workflowapp.Name && app.AppVersion == workflowapp.AppVersion {
						// FIXME: Check if there's a new APP_SDK as well.
						// Skip this check if app_sdk is new.
						if app.Hash == md5 && app.Hash != "" && !forceUpdate {
							skip = true
							break
						}

						//log.Printf("Overriding app %s:%s as it exists but has different hash.", app.Name, app.AppVersion)
						removeApps = append(removeApps, app.ID)
					}
				}

				if skip && !forceUpdate {
					continue
				}

				// Fixes (appends) authentication parameters if they're required
				if workflowapp.Authentication.Required {
					log.Printf("Checking authentication fields and appending for %s!", workflowapp.Name)
					// FIXME:
					// Might require reflection into the python code to append the fields as well
					for index, action := range workflowapp.Actions {
						if action.AuthNotRequired {
							log.Printf("Skipping auth setup: %s", action.Name)
							continue
						}

						// 1. Check if authentication params exists at all
						// 2. Check if they're present in the action
						// 3. Add them IF they DONT exist
						// 4. Fix python code with reflection (FIXME)
						appendParams := []WorkflowAppActionParameter{}
						for _, fieldname := range workflowapp.Authentication.Parameters {
							found := false
							for index, param := range action.Parameters {
								if param.Name == fieldname.Name {
									found = true

									action.Parameters[index].Configuration = true
									//log.Printf("Set config to true for field %s!", param.Name)
									break
								}
							}

							if !found {
								appendParams = append(appendParams, WorkflowAppActionParameter{
									Name:          fieldname.Name,
									Description:   fieldname.Description,
									Example:       fieldname.Example,
									Required:      fieldname.Required,
									Configuration: true,
									Schema:        fieldname.Schema,
								})
							}
						}

						if len(appendParams) > 0 {
							log.Printf("Appending %d params to the START of %s", len(appendParams), action.Name)
							workflowapp.Actions[index].Parameters = append(appendParams, workflowapp.Actions[index].Parameters...)
						}

					}
				}

				err = checkWorkflowApp(workflowapp)
				if err != nil {
					log.Printf("%s for app %s:%s", err, workflowapp.Name, workflowapp.AppVersion)
					continue
				}

				if len(removeApps) > 0 {
					for _, item := range removeApps {
						log.Printf("Removing duplicate: %s", item)
						err = DeleteKey(ctx, "workflowapp", item)
						if err != nil {
							log.Printf("Failed deleting %s", item)
						}
					}
				}

				workflowapp.ID = uuid.NewV4().String()
				workflowapp.IsValid = true
				workflowapp.Verified = true
				workflowapp.Sharing = true
				workflowapp.Downloaded = true
				workflowapp.Hash = md5

				err = setWorkflowAppDatastore(ctx, workflowapp, workflowapp.ID)
				if err != nil {
					log.Printf("Failed setting workflowapp: %s", err)
					continue
				}

				err = increaseStatisticsField(ctx, "total_apps_created", workflowapp.ID, 1)
				if err != nil {
					log.Printf("Failed to increase total apps created stats: %s", err)
				}

				err = increaseStatisticsField(ctx, "total_apps_loaded", workflowapp.ID, 1)
				if err != nil {
					log.Printf("Failed to increase total apps loaded stats: %s", err)
				}

				//log.Printf("Added %s:%s to the database", workflowapp.Name, workflowapp.AppVersion)

				/// Only upload if successful and no errors
				err = buildImageMemory(fs, tags, extra)
				if err != nil {
					log.Printf("Failed image build memory: %s", err)
				} else {
					if len(tags) > 0 {
						log.Printf("Successfully built image %s", tags[0])
					} else {
						log.Printf("Successfully built Docker image")
					}
				}
			}
		}
	}

	return err
}

func setNewWorkflowApp(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	// Just need to be logged in
	// FIXME - should have some permissions?
	_, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new app: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error with body read: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	var workflowapp WorkflowApp
	err = json.Unmarshal(body, &workflowapp)
	if err != nil {
		log.Printf("Failed unmarshaling: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	ctx := context.Background()
	allapps, err := getAllWorkflowApps(ctx)
	if err != nil {
		log.Printf("Failed getting apps to verify: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	appfound := false
	for _, app := range allapps {
		if app.Name == workflowapp.Name && app.AppVersion == workflowapp.AppVersion {
			log.Printf("App upload for %s:%s already exists.", app.Name, app.AppVersion)
			appfound = true
			break
		}
	}

	if appfound {
		log.Printf("App %s:%s already exists. Bump the version.", workflowapp.Name, workflowapp.AppVersion)
		resp.WriteHeader(409)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "App %s:%s already exists."}`, workflowapp.Name, workflowapp.AppVersion)))
		return
	}

	err = checkWorkflowApp(workflowapp)
	if err != nil {
		log.Printf("%s for app %s:%s", err, workflowapp.Name, workflowapp.AppVersion)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s for app %s:%s"}`, err, workflowapp.Name, workflowapp.AppVersion)))
		return
	}

	//if workflowapp.Environment == "" {
	//	workflowapp.Environment = baseEnvironment
	//}

	workflowapp.ID = uuid.NewV4().String()
	workflowapp.IsValid = true
	workflowapp.Generated = false
	workflowapp.Activated = true

	err = setWorkflowAppDatastore(ctx, workflowapp, workflowapp.ID)
	if err != nil {
		log.Printf("Failed setting workflowapp: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	} else {
		log.Printf("Added %s:%s to the database", workflowapp.Name, workflowapp.AppVersion)
	}

	//memcache.Delete(ctx, "all_apps")

	resp.WriteHeader(200)
	resp.Write([]byte(fmt.Sprintf(`{"success": true}`)))
}

func getWorkflowExecutions(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in getting specific workflow: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID when getting workflow executions is not valid"}`))
		return
	}

	ctx := context.Background()
	workflow, err := getWorkflow(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting the workflow locally (get executions): %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// FIXME - have a check for org etc too..
	if user.Id != workflow.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s (get execution)", user.Username, workflow.ID)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// Query for the specifci workflowId
	q := datastore.NewQuery("workflowexecution").Filter("workflow_id =", fileId).Order("-started_at").Limit(20)
	var workflowExecutions []WorkflowExecution
	_, err = dbclient.GetAll(ctx, q, &workflowExecutions)
	if err != nil {
		log.Printf("Error getting workflowexec: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed getting all workflowexecutions for %s"}`, fileId)))
		return
	}

	if len(workflowExecutions) == 0 {
		resp.Write([]byte("[]"))
		resp.WriteHeader(200)
		return
	}

	newjson, err := json.Marshal(workflowExecutions)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "Failed unpacking workflow executions"}`)))
		return
	}

	resp.WriteHeader(200)
	resp.Write(newjson)
}

func getAllSchedules(ctx context.Context) ([]ScheduleOld, error) {
	var schedules []ScheduleOld
	q := datastore.NewQuery("schedules")

	_, err := dbclient.GetAll(ctx, q, &schedules)
	if err != nil {
		return []ScheduleOld{}, err
	}

	return schedules, nil
}

func getAllWorkflowApps(ctx context.Context) ([]WorkflowApp, error) {
	var allworkflowapps []WorkflowApp
	q := datastore.NewQuery("workflowapp")

	_, err := dbclient.GetAll(ctx, q, &allworkflowapps)
	if err != nil {
		return []WorkflowApp{}, err
	}

	return allworkflowapps, nil
}

func getAllWorkflowAppAuth(ctx context.Context) ([]AppAuthenticationStorage, error) {
	var allworkflowapps []AppAuthenticationStorage
	q := datastore.NewQuery("workflowappauth")

	_, err := dbclient.GetAll(ctx, q, &allworkflowapps)
	if err != nil {
		return []AppAuthenticationStorage{}, err
	}

	return allworkflowapps, nil
}

func setWorkflowAppAuthDatastore(ctx context.Context, workflowappauth AppAuthenticationStorage, id string) error {
	key := datastore.NameKey("workflowappauth", id, nil)

	// New struct, to not add body, author etc
	if _, err := dbclient.Put(ctx, key, &workflowappauth); err != nil {
		log.Printf("Error adding workflow app: %s", err)
		return err
	}

	return nil
}

// Hmm, so I guess this should use uuid :(
// Consistency PLX
func setWorkflowAppDatastore(ctx context.Context, workflowapp WorkflowApp, id string) error {
	key := datastore.NameKey("workflowapp", id, nil)

	// New struct, to not add body, author etc
	if _, err := dbclient.Put(ctx, key, &workflowapp); err != nil {
		log.Printf("Error adding workflow app: %s", err)
		return err
	}

	return nil
}

// Starts a new webhook
func handleStopHook(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new workflowhandler: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 32 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID when stopping hook is not valid"}`))
		return
	}

	ctx := context.Background()
	hook, err := getHook(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting hook: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Id != hook.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s", user.Username, hook.Id)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Printf("Status: %s", hook.Status)
	log.Printf("Running: %t", hook.Running)
	if !hook.Running {
		message := fmt.Sprintf("Error: %s isn't running", hook.Id)
		log.Println(message)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, message)))
		return
	}

	hook.Status = "stopped"
	hook.Running = false
	hook.Actions = []HookAction{}
	err = setHook(ctx, *hook)
	if err != nil {
		log.Printf("Failed setting hook: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	image := "webhook"

	// This is here to force stop and remove the old webhook
	// FIXME
	err = removeWebhookFunction(ctx, fileId)
	if err != nil {
		log.Printf("Container stop issue for %s-%s: %s", image, fileId, err)
	}

	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true, "reason": "Stopped webhook"}`))
}

func handleDeleteHook(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new workflowhandler: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID when deleting hook is not valid"}`))
		return
	}

	ctx := context.Background()
	hook, err := getHook(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting hook: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Id != hook.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s", user.Username, hook.Id)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if len(hook.Workflows) > 0 {
		err = increaseStatisticsField(ctx, "total_workflow_triggers", hook.Workflows[0], -1)
		if err != nil {
			log.Printf("Failed to increase total workflows: %s", err)
		}
	}

	hook.Status = "stopped"
	err = setHook(ctx, *hook)
	if err != nil {
		log.Printf("Failed setting hook: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	// This is here to force stop and remove the old webhook
	//image := "webhook"
	//err = removeWebhookFunction(ctx, fileId)
	//if err != nil {
	//	log.Printf("Function removal issue for %s-%s: %s", image, fileId, err)
	//	if strings.Contains(err.Error(), "does not exist") {
	//		resp.WriteHeader(200)
	//		resp.Write([]byte(`{"success": true, "reason": "Stopped webhook"}`))

	//	} else {
	//		resp.WriteHeader(401)
	//		resp.Write([]byte(`{"success": false, "reason": "Couldn't stop webhook, please try again later"}`))
	//	}

	//	return
	//}

	log.Printf("Successfully deleted webhook %s", fileId)
	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true, "reason": "Stopped webhook"}`))
}

func removeWebhookFunction(ctx context.Context, hookid string) error {
	service, err := cloudfunctions.NewService(ctx)
	if err != nil {
		return err
	}

	// ProjectsLocationsListCall
	projectsLocationsFunctionsService := cloudfunctions.NewProjectsLocationsFunctionsService(service)
	location := fmt.Sprintf("projects/%s/locations/%s", gceProject, defaultLocation)
	functionName := fmt.Sprintf("%s/functions/webhook_%s", location, hookid)

	deleteCall := projectsLocationsFunctionsService.Delete(functionName)
	resp, err := deleteCall.Do()
	if err != nil {
		log.Printf("Failed to delete %s from %s: %s", hookid, defaultLocation, err)
		return err
	} else {
		log.Printf("Successfully deleted %s from %s", hookid, defaultLocation)
	}

	_ = resp
	return nil
}

func handleStartHook(resp http.ResponseWriter, request *http.Request) {
	cors := handleCors(resp, request)
	if cors {
		return
	}

	user, err := handleApiAuthentication(resp, request)
	if err != nil {
		log.Printf("Api authentication failed in set new workflowhandler: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	location := strings.Split(request.URL.String(), "/")

	var fileId string
	if location[1] == "api" {
		if len(location) <= 4 {
			resp.WriteHeader(401)
			resp.Write([]byte(`{"success": false}`))
			return
		}

		fileId = location[4]
	}

	if len(fileId) != 36 {
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false, "reason": "Workflow ID when starting hook is not valid"}`))
		return
	}

	ctx := context.Background()
	hook, err := getHook(ctx, fileId)
	if err != nil {
		log.Printf("Failed getting hook: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	if user.Id != hook.Owner && user.Role != "admin" {
		log.Printf("Wrong user (%s) for workflow %s", user.Username, hook.Id)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Printf("Status: %s", hook.Status)
	log.Printf("Running: %t", hook.Running)
	if hook.Running || hook.Status == "Running" {
		message := fmt.Sprintf("Error: %s is already running", hook.Id)
		log.Println(message)
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, message)))
		return
	}

	environmentVariables := map[string]string{
		"FUNCTION_APIKEY": user.ApiKey,
		"CALLBACKURL":     "https://shuffler.io",
		"HOOKID":          fileId,
	}

	applocation := fmt.Sprintf("gs://%s/triggers/webhook.zip", bucketName)
	hookname := fmt.Sprintf("webhook_%s", fileId)
	err = deployWebhookFunction(ctx, hookname, "europe-west2", applocation, environmentVariables)
	if err != nil {
		resp.WriteHeader(401)
		resp.Write([]byte(fmt.Sprintf(`{"success": false, "reason": "%s"}`, err)))
		return
	}

	hook.Status = "running"
	hook.Running = true
	err = setHook(ctx, *hook)
	if err != nil {
		log.Printf("Failed setting hook: %s", err)
		resp.WriteHeader(401)
		resp.Write([]byte(`{"success": false}`))
		return
	}

	log.Printf("Starting function %s?", fileId)
	resp.WriteHeader(200)
	resp.Write([]byte(`{"success": true, "reason": "Started webhook"}`))
	return
}

func removeOutlookTriggerFunction(ctx context.Context, triggerId string) error {
	service, err := cloudfunctions.NewService(ctx)
	if err != nil {
		return err
	}

	// ProjectsLocationsListCall
	projectsLocationsFunctionsService := cloudfunctions.NewProjectsLocationsFunctionsService(service)
	location := fmt.Sprintf("projects/%s/locations/%s", gceProject, defaultLocation)
	functionName := fmt.Sprintf("%s/functions/outlooktrigger_%s", location, triggerId)

	deleteCall := projectsLocationsFunctionsService.Delete(functionName)
	resp, err := deleteCall.Do()
	if err != nil {
		log.Printf("Failed to delete %s from %s: %s", triggerId, defaultLocation, err)
		return err
	} else {
		log.Printf("Successfully deleted %s from %s", triggerId, defaultLocation)
	}

	_ = resp
	return nil
}
