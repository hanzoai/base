package apis

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/tasks"
	"github.com/hanzoai/base/tools/router"
)

// bindTasksApi registers the tasks API endpoints.
// Handlers delegate to the tasks plugin store when registered, or return
// stub responses when the plugin is not loaded.
func bindTasksApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/tasks").Bind(RequireSuperuserAuth())

	// Task CRUD + state transitions.
	sub.GET("", tasksList)
	sub.POST("", tasksCreate)
	sub.POST("/next", tasksNext)

	sub.GET("/{id}", tasksGet)
	sub.PUT("/{id}", tasksUpdate)
	sub.POST("/{id}/claim", tasksClaim)
	sub.POST("/{id}/start", tasksStart)
	sub.POST("/{id}/complete", tasksComplete)
	sub.POST("/{id}/fail", tasksFail)
	sub.POST("/{id}/cancel", tasksCancel)
	sub.POST("/{id}/progress", tasksProgress)
	sub.POST("/{id}/signal", tasksSignal)
	sub.GET("/{id}/query/{queryType}", tasksQuery)

	// Workflows.
	sub.GET("/workflows", workflowsList)
	sub.POST("/workflows", workflowsCreate)
	sub.GET("/workflows/{id}", workflowsGet)
}

// taskStore returns the registered tasks store, or nil.
func taskStore(app core.App) *tasks.Store {
	return tasks.GetStore(app)
}

func notConnected(e *core.RequestEvent) error {
	return e.JSON(http.StatusServiceUnavailable, map[string]any{
		"message": "Tasks plugin not registered. Call tasks.Register(app, config) to enable.",
	})
}

// --- Task Handlers ---

func tasksList(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	filters := parseFilters(e)
	items, err := s.ListTasks(filters)
	if err != nil {
		return e.InternalServerError("Failed to list tasks.", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func tasksCreate(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	var req TaskCreateRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	// Resolve aliases.
	title := req.Title
	if title == "" {
		title = req.Name
	}
	if title == "" {
		return e.BadRequestError("Title (or name) is required.", nil)
	}

	spaceID := req.SpaceID
	if spaceID == "" {
		spaceID = req.Queue
	}
	if spaceID == "" {
		spaceID = "default"
	}

	maxRetries := req.MaxRetries
	if maxRetries == 0 && req.Retry != nil && req.Retry.MaxAttempts > 0 {
		maxRetries = req.Retry.MaxAttempts
	}

	var timeout time.Duration
	if req.TimeoutSecs > 0 {
		timeout = time.Duration(req.TimeoutSecs) * time.Second
	} else if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}

	task := &tasks.Task{
		OrgID:        extractOrgID(e),
		SpaceID:      spaceID,
		Title:        title,
		Description:  req.Description,
		Priority:     tasks.TaskPriority(req.Priority),
		AssignedTo:   req.AssignedTo,
		CreatedBy:    extractCreator(e),
		WorkflowID:   req.WorkflowID,
		ParentTaskID: req.ParentTaskID,
		DependsOn:    req.DependsOn,
		Labels:       req.Labels,
		Input:        req.Input,
		MaxRetries:   maxRetries,
		Timeout:      timeout,
		Metadata:     req.Metadata,
	}

	if err := s.CreateTask(task); err != nil {
		return e.InternalServerError("Failed to create task.", err)
	}

	// Submit to durable execution backend if connected.
	if ds := tasks.GetDurable(e.App); ds != nil && ds.IsConnected() {
		if err := ds.SubmitTask(e.Request.Context(), task); err != nil {
			e.App.Logger().Warn("tasks: durable submit failed, SQLite is authoritative",
				slog.String("task_id", task.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	return e.JSON(http.StatusCreated, task)
}

func tasksGet(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)
	task, err := s.GetTask(id, orgID)
	if err != nil {
		return e.NotFoundError("Task not found.", err)
	}

	return e.JSON(http.StatusOK, task)
}

func tasksUpdate(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)
	task, err := s.GetTask(id, orgID)
	if err != nil {
		return e.NotFoundError("Task not found.", err)
	}

	var req TaskUpdateRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	if req.Title != nil {
		task.Title = *req.Title
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Priority != nil {
		task.Priority = tasks.TaskPriority(*req.Priority)
	}
	if req.Labels != nil {
		task.Labels = req.Labels
	}
	if req.Metadata != nil {
		task.Metadata = req.Metadata
	}

	if err := s.UpdateTask(task); err != nil {
		return e.InternalServerError("Failed to update task.", err)
	}

	return e.JSON(http.StatusOK, task)
}

func tasksClaim(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)

	var req TaskClaimRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if req.AgentID == "" {
		req.AgentID = extractCreator(e)
	}

	if err := s.ClaimTask(id, req.AgentID, orgID); err != nil {
		return mapTaskError(e, err)
	}

	task, _ := s.GetTask(id, orgID)
	return e.JSON(http.StatusOK, task)
}

func tasksStart(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)
	if err := s.StartTask(id, orgID); err != nil {
		return mapTaskError(e, err)
	}

	task, _ := s.GetTask(id, orgID)
	return e.JSON(http.StatusOK, task)
}

func tasksComplete(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)

	var req TaskCompleteRequest
	_ = e.BindBody(&req) // allow empty body

	if err := s.CompleteTask(id, req.Output, orgID); err != nil {
		return mapTaskError(e, err)
	}

	task, _ := s.GetTask(id, orgID)
	return e.JSON(http.StatusOK, task)
}

func tasksFail(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)

	var req TaskFailRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if req.Error == "" {
		return e.BadRequestError("Error message is required.", nil)
	}

	if err := s.FailTask(id, req.Error, orgID); err != nil {
		return mapTaskError(e, err)
	}

	task, _ := s.GetTask(id, orgID)
	return e.JSON(http.StatusOK, task)
}

func tasksCancel(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)
	if err := s.CancelTask(id, orgID); err != nil {
		return mapTaskError(e, err)
	}

	// Also cancel the durable workflow if connected.
	if ds := tasks.GetDurable(e.App); ds != nil && ds.IsConnected() {
		if err := ds.CancelTask(e.Request.Context(), id, orgID); err != nil {
			e.App.Logger().Warn("tasks: durable cancel failed",
				slog.String("task_id", id),
				slog.String("error", err.Error()),
			)
		}
	}

	task, _ := s.GetTask(id, orgID)
	return e.JSON(http.StatusOK, task)
}

func tasksProgress(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)

	var req TaskProgressRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	if err := s.UpdateProgress(id, req.Progress, orgID); err != nil {
		return mapTaskError(e, err)
	}

	task, _ := s.GetTask(id, orgID)
	return e.JSON(http.StatusOK, task)
}

func tasksNext(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	var req TaskNextRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	spaceID := req.SpaceID
	if spaceID == "" {
		spaceID = req.Queue
	}
	if spaceID == "" {
		spaceID = "default"
	}
	if req.AgentID == "" {
		req.AgentID = extractCreator(e)
	}

	orgID := extractOrgID(e)
	task, err := s.GetNextPendingTask(spaceID, req.AgentID, orgID)
	if err != nil {
		return e.InternalServerError("Failed to get next task.", err)
	}
	if task == nil {
		return e.JSON(http.StatusNoContent, nil)
	}

	return e.JSON(http.StatusOK, task)
}

func tasksSignal(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)

	var req TaskSignalRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if req.Name == "" {
		return e.BadRequestError("Signal name is required.", nil)
	}

	allowedSignals := map[string]bool{
		"claim": true, "complete": true, "fail": true,
		"progress": true, "update": true, "cancel": true,
	}
	if !allowedSignals[req.Name] {
		return e.BadRequestError("Invalid signal name. Allowed: claim, complete, fail, progress, update, cancel.", nil)
	}

	// Verify task exists and belongs to the org.
	if _, err := s.GetTask(id, orgID); err != nil {
		return e.NotFoundError("Task not found.", err)
	}

	ds := tasks.GetDurable(e.App)
	if ds == nil || !ds.IsConnected() {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"message": "Task signaling requires durable execution (Temporal). Enable with TASKS_ENABLED=true.",
		})
	}

	if err := ds.SignalTask(e.Request.Context(), id, req.Name, req.Data, orgID); err != nil {
		return e.InternalServerError("Failed to signal task.", err)
	}

	return e.JSON(http.StatusOK, map[string]any{"signaled": true, "task_id": id, "signal": req.Name})
}

func tasksQuery(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)
	task, err := s.GetTask(id, orgID)
	if err != nil {
		return e.NotFoundError("Task not found.", err)
	}

	// Return task state as the query result.
	return e.JSON(http.StatusOK, map[string]any{
		"task_id": task.ID,
		"state":   task.State,
		"output":  task.Output,
	})
}

// --- Workflow Handlers ---

func workflowsList(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	spaceID := e.Request.URL.Query().Get("space_id")
	if spaceID == "" {
		spaceID = e.Request.URL.Query().Get("queue")
	}
	orgID := extractOrgID(e)

	items, err := s.ListWorkflows(spaceID, orgID)
	if err != nil {
		return e.InternalServerError("Failed to list workflows.", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func workflowsCreate(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	var req WorkflowCreateRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if req.Name == "" {
		return e.BadRequestError("Workflow name is required.", nil)
	}

	spaceID := req.SpaceID
	if spaceID == "" {
		spaceID = req.Queue
	}
	if spaceID == "" {
		spaceID = "default"
	}

	taskDefs := req.Tasks
	if len(taskDefs) == 0 {
		taskDefs = req.Steps
	}
	if len(taskDefs) == 0 {
		return e.BadRequestError("Workflow must have at least one task.", nil)
	}

	createdBy := extractCreator(e)
	orgID := extractOrgID(e)

	// Create the workflow first to get its ID.
	wf := &tasks.Workflow{
		OrgID:       orgID,
		SpaceID:     spaceID,
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   createdBy,
		Metadata:    req.Metadata,
	}
	if err := s.CreateWorkflow(wf); err != nil {
		return e.InternalServerError("Failed to create workflow.", err)
	}

	// Create tasks linked to this workflow.
	taskIDs := make([]string, 0, len(taskDefs))
	for i, td := range taskDefs {
		title := td.Title
		if title == "" {
			title = td.Name
		}

		task := &tasks.Task{
			OrgID:       orgID,
			SpaceID:     spaceID,
			Title:       title,
			Description: td.Description,
			Priority:    tasks.TaskPriority(td.Priority),
			CreatedBy:   createdBy,
			WorkflowID:  wf.ID,
			DependsOn:   td.DependsOn,
			Labels:      td.Labels,
			Input:       td.Input,
			MaxRetries:  td.MaxRetries,
			Metadata:    td.Metadata,
		}

		// Auto-chain: each task depends on the previous unless explicit deps set.
		if !req.Parallel && len(task.DependsOn) == 0 && i > 0 {
			task.DependsOn = []string{taskIDs[i-1]}
		}

		if td.TimeoutSecs > 0 {
			task.Timeout = time.Duration(td.TimeoutSecs) * time.Second
		}

		if err := s.CreateTask(task); err != nil {
			return e.InternalServerError("Failed to create workflow task.", err)
		}
		taskIDs = append(taskIDs, task.ID)
	}

	// Update workflow with task IDs.
	wf.Tasks = taskIDs
	if err := s.UpdateWorkflowTasks(wf); err != nil {
		return e.InternalServerError("Failed to link workflow tasks.", err)
	}

	// Return workflow with tasks.
	taskList := make([]*tasks.Task, 0, len(taskIDs))
	for _, tid := range taskIDs {
		if t, err := s.GetTask(tid); err == nil {
			taskList = append(taskList, t)
		}
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"workflow": wf,
		"tasks":    taskList,
	})
}

func workflowsGet(e *core.RequestEvent) error {
	s := taskStore(e.App)
	if s == nil {
		return notConnected(e)
	}

	id := e.Request.PathValue("id")
	orgID := extractOrgID(e)
	wf, err := s.GetWorkflow(id, orgID)
	if err != nil {
		return e.NotFoundError("Workflow not found.", err)
	}

	// Attach task details.
	taskList := make([]*tasks.Task, 0, len(wf.Tasks))
	for _, tid := range wf.Tasks {
		if t, err := s.GetTask(tid, orgID); err == nil {
			taskList = append(taskList, t)
		}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"workflow": wf,
		"tasks":    taskList,
	})
}

// --- Helpers ---

func extractCreator(e *core.RequestEvent) string {
	if e.Auth != nil {
		return e.Auth.Id
	}
	return "anonymous"
}

// extractOrgID returns the org ID from gateway-injected header.
func extractOrgID(e *core.RequestEvent) string {
	if v := e.Request.Header.Get("X-Org-Id"); v != "" {
		return v
	}
	return ""
}

func parseFilters(e *core.RequestEvent) tasks.TaskFilters {
	q := e.Request.URL.Query()
	var f tasks.TaskFilters

	f.OrgID = extractOrgID(e)

	f.SpaceID = q.Get("space_id")
	if f.SpaceID == "" {
		f.SpaceID = q.Get("queue")
	}

	if s := q.Get("state"); s != "" {
		state := tasks.TaskState(s)
		f.State = &state
	}
	if a := q.Get("assigned_to"); a != "" {
		f.AssignedTo = &a
	}
	if p := q.Get("priority"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			prio := tasks.TaskPriority(v)
			f.Priority = &prio
		}
	}
	if w := q.Get("workflow_id"); w != "" {
		f.WorkflowID = &w
	}
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			f.Limit = v
		}
	}
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			f.Offset = v
		}
	}

	return f
}

func mapTaskError(e *core.RequestEvent, err error) error {
	switch err {
	case tasks.ErrTaskNotFound:
		return e.NotFoundError("Task not found.", err)
	case tasks.ErrInvalidTransition, tasks.ErrAlreadyClaimed:
		return e.JSON(http.StatusConflict, map[string]any{"error": err.Error()})
	default:
		return e.InternalServerError("Task operation failed.", err)
	}
}
