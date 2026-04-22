package cron

import (
	"encoding/json"

	"github.com/hanzoai/base/tools/tasks"
)

// Job is a read-only view of a registered schedule, returned by Cron.Jobs().
type Job struct {
	id         string
	expression string
	client     *tasks.Client
}

// Id returns the job id.
func (j *Job) Id() string { return j.id }

// Expression returns the cron expression or duration string the job was
// registered with.
func (j *Job) Expression() string { return j.expression }

// Run invokes the job's registered callback immediately, outside the normal
// schedule. No-op if the callback runs server-side (durable tasks) and is
// therefore not available in-process.
func (j *Job) Run() {
	if j.client == nil {
		return
	}
	j.client.Run(j.id)
}

// MarshalJSON implements json.Marshaler for the admin list endpoint.
func (j Job) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Id         string `json:"id"`
		Expression string `json:"expression"`
	}{Id: j.id, Expression: j.expression})
}
