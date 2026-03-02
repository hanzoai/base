package apis

import (
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// bindTasksApi registers the tasks API endpoints.
// These provide durable workflow execution backed by Hanzo Tasks (Temporal).
func bindTasksApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	subGroup := rg.Group("/tasks").Bind(RequireSuperuserAuth())

	// List tasks (filterable by state, queue, assignee)
	subGroup.GET("", tasksList)

	// Submit a new task
	subGroup.POST("", tasksSubmit)

	// Get task details
	subGroup.GET("/{id}", tasksGet)

	// Cancel a task
	subGroup.POST("/{id}/cancel", tasksCancel)

	// Signal a running task
	subGroup.POST("/{id}/signal", tasksSignal)

	// Query a running task's state
	subGroup.GET("/{id}/query/{queryType}", tasksQuery)

	// List workflows
	subGroup.GET("/workflows", workflowsList)

	// Submit a workflow
	subGroup.POST("/workflows", workflowsSubmit)

	// Get workflow details + history
	subGroup.GET("/workflows/{id}", workflowsGet)
}

func tasksList(e *core.RequestEvent) error {
	state := e.Request.URL.Query().Get("state")
	queue := e.Request.URL.Query().Get("queue")

	return e.JSON(http.StatusOK, map[string]any{
		"items": []TaskResponse{},
		"filter": map[string]string{
			"state": state,
			"queue": queue,
		},
		"message": "Tasks backend not connected. Wire a Temporal client via the OnServe hook.",
	})
}

func tasksSubmit(e *core.RequestEvent) error {
	var req TaskSubmitRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Failed to read task request body.", err)
	}

	if req.Name == "" {
		return e.BadRequestError("Task name is required.", nil)
	}
	if req.Queue == "" {
		req.Queue = "default"
	}

	return e.JSON(http.StatusAccepted, TaskResponse{
		ID:      "",
		Name:    req.Name,
		Queue:   req.Queue,
		State:   "pending",
		Input:   req.Input,
		Created: "",
	})
}

func tasksGet(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	if id == "" {
		return e.BadRequestError("Task id is required.", nil)
	}

	return e.NotFoundError("Task not found. Tasks backend not connected.", nil)
}

func tasksCancel(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	if id == "" {
		return e.BadRequestError("Task id is required.", nil)
	}

	return e.NotFoundError("Task not found. Tasks backend not connected.", nil)
}

func tasksSignal(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	if id == "" {
		return e.BadRequestError("Task id is required.", nil)
	}

	var req TaskSignalRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Failed to read signal request body.", err)
	}

	if req.Name == "" {
		return e.BadRequestError("Signal name is required.", nil)
	}

	return e.NotFoundError("Task not found. Tasks backend not connected.", nil)
}

func tasksQuery(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	if id == "" {
		return e.BadRequestError("Task id is required.", nil)
	}

	queryType := e.Request.PathValue("queryType")
	if queryType == "" {
		return e.BadRequestError("Query type is required.", nil)
	}

	return e.NotFoundError("Task not found. Tasks backend not connected.", nil)
}

func workflowsList(e *core.RequestEvent) error {
	state := e.Request.URL.Query().Get("state")
	queue := e.Request.URL.Query().Get("queue")

	return e.JSON(http.StatusOK, map[string]any{
		"items": []WorkflowResponse{},
		"filter": map[string]string{
			"state": state,
			"queue": queue,
		},
		"message": "Tasks backend not connected. Wire a Temporal client via the OnServe hook.",
	})
}

func workflowsSubmit(e *core.RequestEvent) error {
	var req WorkflowSubmitRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Failed to read workflow request body.", err)
	}

	if req.Name == "" {
		return e.BadRequestError("Workflow name is required.", nil)
	}
	if req.Queue == "" {
		req.Queue = "default"
	}

	return e.JSON(http.StatusAccepted, WorkflowResponse{
		ID:      "",
		Name:    req.Name,
		Queue:   req.Queue,
		State:   "pending",
		Input:   req.Input,
		Created: "",
	})
}

func workflowsGet(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	if id == "" {
		return e.BadRequestError("Workflow id is required.", nil)
	}

	return e.NotFoundError("Workflow not found. Tasks backend not connected.", nil)
}
