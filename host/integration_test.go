// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package host

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/stretchr/testify/suite"
	"github.com/uber-common/bark"
	tchannel "github.com/uber/tchannel-go"

	"bytes"
	"encoding/binary"
	"strconv"

	workflow "github.com/uber/cadence/.gen/go/shared"
	"github.com/uber/cadence/client/frontend"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/service/history"
	"github.com/uber/cadence/service/matching"
)

var (
	integration = flag.Bool("integration", true, "run integration tests")
)

const (
	testNumberOfHistoryShards = 4
)

type (
	integrationSuite struct {
		host   Cadence
		ch     *tchannel.Channel
		engine frontend.Client
		logger bark.Logger
		suite.Suite
		persistence.TestBase
	}

	decisionTaskHandler func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision)
	activityTaskHandler func(execution *workflow.WorkflowExecution, activityType *workflow.ActivityType,
		activityID string, startedEventID int64, input []byte, takeToken []byte) ([]byte, bool, error)

	taskPoller struct {
		engine          frontend.Client
		taskList        *workflow.TaskList
		identity        string
		decisionHandler decisionTaskHandler
		activityHandler activityTaskHandler
		logger          bark.Logger
	}
)

func TestIntegrationSuite(t *testing.T) {
	flag.Parse()
	if *integration {
		s := new(integrationSuite)
		suite.Run(t, s)
	} else {
		t.Skip()
	}
}

func (s *integrationSuite) SetupSuite() {
	if testing.Verbose() {
		log.SetOutput(os.Stdout)
	}

	logger := log.New()
	logger.Level = log.DebugLevel
	s.logger = bark.NewLoggerFromLogrus(logger)

	s.ch, _ = tchannel.NewChannel("cadence-integration-test", nil)
}

func (s *integrationSuite) TearDownSuite() {
}

func (s *integrationSuite) SetupTest() {
	options := persistence.TestBaseOptions{}
	options.ClusterHost = "127.0.0.1"
	options.DropKeySpace = true
	options.SchemaDir = ".."
	s.SetupWorkflowStoreWithOptions(options)

	s.setupShards()

	s.host = NewCadence(s.ShardMgr, s.ExecutionMgrFactory, s.TaskMgr, testNumberOfHistoryShards, s.logger)
	s.host.Start()
	s.engine, _ = frontend.NewClient(s.ch, s.host.FrontendAddress())
}

func (s *integrationSuite) TearDownTest() {
	s.host.Stop()
	s.host = nil
	s.TearDownWorkflowStore()
}

func (s *integrationSuite) TestIntegrationStartWorkflowExecution() {
	id := "integration-start-workflow-test"
	wt := "integration-start-workflow-test-type"
	tl := "integration-start-workflow-test-tasklist"
	identity := "worker1"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	_, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	we1, err1 := s.engine.StartWorkflowExecution(request)
	s.NotNil(err1)
	s.IsType(workflow.NewWorkflowExecutionAlreadyStartedError(), err1)
	log.Infof("Unable to start workflow execution: %v", err1.Error())
	s.Nil(we1)
}

func (s *integrationSuite) TestSequentialWorkflow() {
	id := "interation-sequential-workflow-test"
	wt := "interation-sequential-workflow-test-type"
	tl := "interation-sequential-workflow-test-tasklist"
	identity := "worker1"
	activityName := "activity_type1"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	we, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	s.logger.Infof("StartWorkflowExecution: response: %v \n", we.GetRunId())

	workflowComplete := false
	activityCount := int32(10)
	activityCounter := int32(0)
	dtHandler := func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision) {
		if activityCounter < activityCount {
			activityCounter++
			buf := new(bytes.Buffer)
			s.Nil(binary.Write(buf, binary.LittleEndian, activityCounter))

			return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_ScheduleActivityTask),
				ScheduleActivityTaskDecisionAttributes: &workflow.ScheduleActivityTaskDecisionAttributes{
					ActivityId:   common.StringPtr(strconv.Itoa(int(activityCounter))),
					ActivityType: &workflow.ActivityType{Name: common.StringPtr(activityName)},
					TaskList:     &workflow.TaskList{Name: &tl},
					Input:        buf.Bytes(),
					ScheduleToCloseTimeoutSeconds: common.Int32Ptr(100),
					ScheduleToStartTimeoutSeconds: common.Int32Ptr(10),
					StartToCloseTimeoutSeconds:    common.Int32Ptr(50),
					HeartbeatTimeoutSeconds:       common.Int32Ptr(5),
				},
			}}
		}

		workflowComplete = true
		return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
			DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_CompleteWorkflowExecution),
			CompleteWorkflowExecutionDecisionAttributes: &workflow.CompleteWorkflowExecutionDecisionAttributes{
				Result_: []byte("Done."),
			},
		}}
	}

	expectedActivity := int32(1)
	atHandler := func(execution *workflow.WorkflowExecution, activityType *workflow.ActivityType,
		activityID string, startedEventID int64, input []byte, taskToken []byte) ([]byte, bool, error) {
		s.Equal(id, execution.GetWorkflowId())
		s.Equal(activityName, activityType.GetName())
		id, _ := strconv.Atoi(activityID)
		s.Equal(int(expectedActivity), id)
		s.logger.Errorf("Input: %v", input)
		buf := bytes.NewReader(input)
		var in int32
		binary.Read(buf, binary.LittleEndian, &in)
		s.Equal(expectedActivity, in)
		expectedActivity++

		return []byte("Activity Result."), false, nil
	}

	poller := &taskPoller{
		engine:          s.engine,
		taskList:        taskList,
		identity:        identity,
		decisionHandler: dtHandler,
		activityHandler: atHandler,
		logger:          s.logger,
	}

	for i := 0; i < 10; i++ {
		err := poller.pollAndProcessDecisionTask(false, false)
		s.logger.Infof("pollAndProcessDecisionTask: %v", err)
		s.Nil(err)
		err = poller.pollAndProcessActivityTask(false)
		s.logger.Infof("pollAndProcessActivityTask: %v", err)
		s.Nil(err)
	}

	s.False(workflowComplete)
	s.Nil(poller.pollAndProcessDecisionTask(true, false))
	s.True(workflowComplete)
}

func (p *taskPoller) pollAndProcessDecisionTask(dumpHistory bool, dropTask bool) error {
retry:
	for attempt := 0; attempt < 5; attempt++ {
		response, err1 := p.engine.PollForDecisionTask(&workflow.PollForDecisionTaskRequest{
			TaskList: p.taskList,
			Identity: common.StringPtr(p.identity),
		})

		if err1 == history.ErrDuplicate {
			p.logger.Info("Duplicate Decision task: Polling again.")
			continue retry
		}

		if err1 != nil {
			return err1
		}

		if response == nil || len(response.TaskToken) == 0 {
			p.logger.Info("Empty Decision task: Polling again.")
			continue retry
		}

		if dropTask {
			p.logger.Info("Dropping Decision task: ")
			return nil
		}

		if dumpHistory {
			common.PrettyPrintHistory(response.GetHistory(), p.logger)
		}

		context, decisions := p.decisionHandler(response.GetWorkflowExecution(), response.GetWorkflowType(),
			response.GetPreviousStartedEventId(), response.GetStartedEventId(), response.GetHistory())

		return p.engine.RespondDecisionTaskCompleted(&workflow.RespondDecisionTaskCompletedRequest{
			TaskToken:        response.GetTaskToken(),
			Identity:         common.StringPtr(p.identity),
			ExecutionContext: context,
			Decisions:        decisions,
		})
	}

	return matching.ErrNoTasks
}

func (p *taskPoller) pollAndProcessActivityTask(dropTask bool) error {
retry:
	for attempt := 0; attempt < 5; attempt++ {
		response, err1 := p.engine.PollForActivityTask(&workflow.PollForActivityTaskRequest{
			TaskList: p.taskList,
			Identity: common.StringPtr(p.identity),
		})

		if err1 == history.ErrDuplicate {
			p.logger.Info("Duplicate Activity task: Polling again.")
			continue retry
		}

		if err1 != nil {
			return err1
		}

		if response == nil || len(response.TaskToken) == 0 {
			p.logger.Info("Empty Activity task: Polling again.")
			return nil
		}

		if dropTask {
			p.logger.Info("Dropping Activity task: ")
			return nil
		}
		p.logger.Infof("Received Activity task: %v", response)

		result, cancel, err2 := p.activityHandler(response.GetWorkflowExecution(), response.GetActivityType(), response.GetActivityId(),
			response.GetStartedEventId(), response.GetInput(), response.GetTaskToken())
		if cancel {
			p.logger.Info("Executing RespondActivityTaskCanceled")
			return p.engine.RespondActivityTaskCanceled(&workflow.RespondActivityTaskCanceledRequest{
				TaskToken: response.GetTaskToken(),
				Details:   []byte("details"),
				Identity:  common.StringPtr(p.identity),
			})
		}

		if err2 != nil {
			return p.engine.RespondActivityTaskFailed(&workflow.RespondActivityTaskFailedRequest{
				TaskToken: response.GetTaskToken(),
				Reason:    common.StringPtr(err2.Error()),
				Identity:  common.StringPtr(p.identity),
			})
		}

		return p.engine.RespondActivityTaskCompleted(&workflow.RespondActivityTaskCompletedRequest{
			TaskToken: response.GetTaskToken(),
			Identity:  common.StringPtr(p.identity),
			Result_:   result,
		})
	}

	return matching.ErrNoTasks
}

func (s *integrationSuite) TestDecisionAndActivityTimeoutsWorkflow() {
	id := "interation-timeouts-workflow-test"
	wt := "interation-timeouts-workflow-test-type"
	tl := "interation-timeouts-workflow-test-tasklist"
	identity := "worker1"
	activityName := "activity_timer"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	we, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	s.logger.Infof("StartWorkflowExecution: response: %v \n", we.GetRunId())

	workflowComplete := false
	activityCount := int32(10)
	activityCounter := int32(0)

	dtHandler := func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision) {
		if activityCounter < activityCount {
			activityCounter++
			buf := new(bytes.Buffer)
			s.Nil(binary.Write(buf, binary.LittleEndian, activityCounter))

			return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_ScheduleActivityTask),
				ScheduleActivityTaskDecisionAttributes: &workflow.ScheduleActivityTaskDecisionAttributes{
					ActivityId:   common.StringPtr(strconv.Itoa(int(activityCounter))),
					ActivityType: &workflow.ActivityType{Name: common.StringPtr(activityName)},
					TaskList:     &workflow.TaskList{Name: &tl},
					Input:        buf.Bytes(),
					ScheduleToCloseTimeoutSeconds: common.Int32Ptr(1),
					ScheduleToStartTimeoutSeconds: common.Int32Ptr(1),
					StartToCloseTimeoutSeconds:    common.Int32Ptr(1),
					HeartbeatTimeoutSeconds:       common.Int32Ptr(1),
				},
			}}
		}

		s.logger.Info("Completing Workflow.")

		workflowComplete = true
		return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
			DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_CompleteWorkflowExecution),
			CompleteWorkflowExecutionDecisionAttributes: &workflow.CompleteWorkflowExecutionDecisionAttributes{
				Result_: []byte("Done."),
			},
		}}
	}

	atHandler := func(execution *workflow.WorkflowExecution, activityType *workflow.ActivityType,
		activityID string, startedEventID int64, input []byte, taskToken []byte) ([]byte, bool, error) {
		s.Equal(id, execution.GetWorkflowId())
		s.Equal(activityName, activityType.GetName())
		s.logger.Errorf("Activity ID: %v", activityID)
		return []byte("Activity Result."), false, nil
	}

	poller := &taskPoller{
		engine:          s.engine,
		taskList:        taskList,
		identity:        identity,
		decisionHandler: dtHandler,
		activityHandler: atHandler,
		logger:          s.logger,
	}

	for i := 0; i < 20; i++ {
		dropDecisionTask := (i%2 == 0)
		s.logger.Infof("Calling Decision Task: %d", i)
		err := poller.pollAndProcessDecisionTask(false, dropDecisionTask)
		s.True(err == nil || err == matching.ErrNoTasks)
		if !dropDecisionTask {
			s.logger.Infof("Calling Activity Task: %d", i)
			err = poller.pollAndProcessActivityTask(i%4 == 0)
			s.True(err == nil || err == matching.ErrNoTasks)
		}
	}

	s.logger.Infof("Waiting for workflow to complete: RunId: %v", we.GetRunId())

	s.False(workflowComplete)
	s.Nil(poller.pollAndProcessDecisionTask(true, false))
	s.True(workflowComplete)
}

func (s *integrationSuite) TestActivityHeartBeatWorkflow_Success() {
	id := "integration-heartbeat-test"
	wt := "integration-heartbeat-test-type"
	tl := "integration-heartbeat-test-tasklist"
	identity := "worker1"
	activityName := "activity_timer"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	we, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	s.logger.Infof("StartWorkflowExecution: response: %v \n", we.GetRunId())

	workflowComplete := false
	activityCount := int32(1)
	activityCounter := int32(0)

	dtHandler := func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision) {
		if activityCounter < activityCount {
			activityCounter++
			buf := new(bytes.Buffer)
			s.Nil(binary.Write(buf, binary.LittleEndian, activityCounter))

			return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_ScheduleActivityTask),
				ScheduleActivityTaskDecisionAttributes: &workflow.ScheduleActivityTaskDecisionAttributes{
					ActivityId:   common.StringPtr(strconv.Itoa(int(activityCounter))),
					ActivityType: &workflow.ActivityType{Name: common.StringPtr(activityName)},
					TaskList:     &workflow.TaskList{Name: &tl},
					Input:        buf.Bytes(),
					ScheduleToCloseTimeoutSeconds: common.Int32Ptr(15),
					ScheduleToStartTimeoutSeconds: common.Int32Ptr(1),
					StartToCloseTimeoutSeconds:    common.Int32Ptr(15),
					HeartbeatTimeoutSeconds:       common.Int32Ptr(1),
				},
			}}
		}

		s.logger.Info("Completing Workflow.")

		workflowComplete = true
		return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
			DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_CompleteWorkflowExecution),
			CompleteWorkflowExecutionDecisionAttributes: &workflow.CompleteWorkflowExecutionDecisionAttributes{
				Result_: []byte("Done."),
			},
		}}
	}

	activityExecutedCount := 0
	atHandler := func(execution *workflow.WorkflowExecution, activityType *workflow.ActivityType,
		activityID string, startedEventID int64, input []byte, taskToken []byte) ([]byte, bool, error) {
		s.Equal(id, execution.GetWorkflowId())
		s.Equal(activityName, activityType.GetName())
		for i := 0; i < 10; i++ {
			s.logger.Infof("Heartbeating for activity: %s, count: %d", activityID, i)
			_, err := s.engine.RecordActivityTaskHeartbeat(&workflow.RecordActivityTaskHeartbeatRequest{
				TaskToken: taskToken, Details: []byte("details")})
			s.Nil(err)
			time.Sleep(10 * time.Millisecond)
		}
		activityExecutedCount++
		return []byte("Activity Result."), false, nil
	}

	poller := &taskPoller{
		engine:          s.engine,
		taskList:        taskList,
		identity:        identity,
		decisionHandler: dtHandler,
		activityHandler: atHandler,
		logger:          s.logger,
	}

	err := poller.pollAndProcessDecisionTask(false, false)
	s.True(err == nil || err == matching.ErrNoTasks)

	err = poller.pollAndProcessActivityTask(false)
	s.True(err == nil || err == matching.ErrNoTasks)

	s.logger.Infof("Waiting for workflow to complete: RunId: %v", we.GetRunId())

	s.False(workflowComplete)
	s.Nil(poller.pollAndProcessDecisionTask(true, false))
	s.True(workflowComplete)
	s.True(activityExecutedCount == 1)
}

func (s *integrationSuite) TestActivityHeartBeatWorkflow_Timeout() {
	id := "integration-heartbeat-timeout-test"
	wt := "integration-heartbeat-timeout-test-type"
	tl := "integration-heartbeat-timeout-test-tasklist"
	identity := "worker1"
	activityName := "activity_timer"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	we, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	s.logger.Infof("StartWorkflowExecution: response: %v \n", we.GetRunId())

	workflowComplete := false
	activityCount := int32(1)
	activityCounter := int32(0)

	dtHandler := func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision) {

		s.logger.Infof("Calling DecisionTask Handler: %d, %d.", activityCounter, activityCount)

		if activityCounter < activityCount {
			activityCounter++
			buf := new(bytes.Buffer)
			s.Nil(binary.Write(buf, binary.LittleEndian, activityCounter))

			return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_ScheduleActivityTask),
				ScheduleActivityTaskDecisionAttributes: &workflow.ScheduleActivityTaskDecisionAttributes{
					ActivityId:   common.StringPtr(strconv.Itoa(int(activityCounter))),
					ActivityType: &workflow.ActivityType{Name: common.StringPtr(activityName)},
					TaskList:     &workflow.TaskList{Name: &tl},
					Input:        buf.Bytes(),
					ScheduleToCloseTimeoutSeconds: common.Int32Ptr(15),
					ScheduleToStartTimeoutSeconds: common.Int32Ptr(1),
					StartToCloseTimeoutSeconds:    common.Int32Ptr(15),
					HeartbeatTimeoutSeconds:       common.Int32Ptr(1),
				},
			}}
		}

		workflowComplete = true
		return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
			DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_CompleteWorkflowExecution),
			CompleteWorkflowExecutionDecisionAttributes: &workflow.CompleteWorkflowExecutionDecisionAttributes{
				Result_: []byte("Done."),
			},
		}}
	}

	activityExecutedCount := 0
	atHandler := func(execution *workflow.WorkflowExecution, activityType *workflow.ActivityType,
		activityID string, startedEventID int64, input []byte, taskToken []byte) ([]byte, bool, error) {
		s.Equal(id, execution.GetWorkflowId())
		s.Equal(activityName, activityType.GetName())
		// Timing out more than HB time.
		time.Sleep(2 * time.Second)
		activityExecutedCount++
		return []byte("Activity Result."), false, nil
	}

	poller := &taskPoller{
		engine:          s.engine,
		taskList:        taskList,
		identity:        identity,
		decisionHandler: dtHandler,
		activityHandler: atHandler,
		logger:          s.logger,
	}

	err := poller.pollAndProcessDecisionTask(false, false)
	s.True(err == nil || err == matching.ErrNoTasks)

	err = poller.pollAndProcessActivityTask(false)

	s.logger.Infof("Waiting for workflow to complete: RunId: %v", we.GetRunId())

	s.False(workflowComplete)
	s.Nil(poller.pollAndProcessDecisionTask(true, false))
	s.True(workflowComplete)
}

func (s *integrationSuite) TestSequential_UserTimers() {
	id := "interation-sequential-user-timers-test"
	wt := "interation-sequential-user-timers-test-type"
	tl := "interation-sequential-user-timers-test-tasklist"
	identity := "worker1"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	we, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	s.logger.Infof("StartWorkflowExecution: response: %v \n", we.GetRunId())

	workflowComplete := false
	timerCount := int32(10)
	timerCounter := int32(0)
	dtHandler := func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision) {
		if timerCounter < timerCount {
			timerCounter++
			buf := new(bytes.Buffer)
			s.Nil(binary.Write(buf, binary.LittleEndian, timerCounter))

			return []byte(strconv.Itoa(int(timerCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_StartTimer),
				StartTimerDecisionAttributes: &workflow.StartTimerDecisionAttributes{
					TimerId:                   common.StringPtr(fmt.Sprintf("timer-id-%d", timerCounter)),
					StartToFireTimeoutSeconds: common.Int64Ptr(1),
				},
			}}
		}

		workflowComplete = true
		return []byte(strconv.Itoa(int(timerCounter))), []*workflow.Decision{{
			DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_CompleteWorkflowExecution),
			CompleteWorkflowExecutionDecisionAttributes: &workflow.CompleteWorkflowExecutionDecisionAttributes{
				Result_: []byte("Done."),
			},
		}}
	}

	poller := &taskPoller{
		engine:          s.engine,
		taskList:        taskList,
		identity:        identity,
		decisionHandler: dtHandler,
		activityHandler: nil,
		logger:          s.logger,
	}

	for i := 0; i < 10; i++ {
		err := poller.pollAndProcessDecisionTask(false, false)
		s.logger.Infof("pollAndProcessDecisionTask: %v", err)
		s.Nil(err)
	}

	s.False(workflowComplete)
	s.Nil(poller.pollAndProcessDecisionTask(true, false))
	s.True(workflowComplete)
}

func (s *integrationSuite) TestActivityCancelation() {
	id := "integration-activity-cancelation-test"
	wt := "integration-activity-cancelation-test-type"
	tl := "integration-activity-cancelation-test-tasklist"
	identity := "worker1"
	activityName := "activity_timer"

	workflowType := workflow.NewWorkflowType()
	workflowType.Name = common.StringPtr(wt)

	taskList := workflow.NewTaskList()
	taskList.Name = common.StringPtr(tl)

	request := &workflow.StartWorkflowExecutionRequest{
		WorkflowId:   common.StringPtr(id),
		WorkflowType: workflowType,
		TaskList:     taskList,
		Input:        nil,
		ExecutionStartToCloseTimeoutSeconds: common.Int32Ptr(100),
		TaskStartToCloseTimeoutSeconds:      common.Int32Ptr(1),
		Identity:                            common.StringPtr(identity),
	}

	we, err0 := s.engine.StartWorkflowExecution(request)
	s.Nil(err0)

	s.logger.Infof("StartWorkflowExecution: response: %v \n", we.GetRunId())

	workflowComplete := false
	activityCounter := int32(0)
	scheduleActivity := true
	requestCancellation := false

	dtHandler := func(execution *workflow.WorkflowExecution, wt *workflow.WorkflowType,
		previousStartedEventID, startedEventID int64, history *workflow.History) ([]byte, []*workflow.Decision) {
		if scheduleActivity {
			activityCounter++
			buf := new(bytes.Buffer)
			s.Nil(binary.Write(buf, binary.LittleEndian, activityCounter))

			return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_ScheduleActivityTask),
				ScheduleActivityTaskDecisionAttributes: &workflow.ScheduleActivityTaskDecisionAttributes{
					ActivityId:   common.StringPtr(strconv.Itoa(int(activityCounter))),
					ActivityType: &workflow.ActivityType{Name: common.StringPtr(activityName)},
					TaskList:     &workflow.TaskList{Name: &tl},
					Input:        buf.Bytes(),
					ScheduleToCloseTimeoutSeconds: common.Int32Ptr(15),
					ScheduleToStartTimeoutSeconds: common.Int32Ptr(10),
					StartToCloseTimeoutSeconds:    common.Int32Ptr(15),
					HeartbeatTimeoutSeconds:       common.Int32Ptr(0),
				},
			}}
		}

		if requestCancellation {
			return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
				DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_RequestCancelActivityTask),
				RequestCancelActivityTaskDecisionAttributes: &workflow.RequestCancelActivityTaskDecisionAttributes{
					ActivityId: common.StringPtr(strconv.Itoa(int(activityCounter))),
				},
			}}
		}

		s.logger.Info("Completing Workflow.")

		workflowComplete = true
		return []byte(strconv.Itoa(int(activityCounter))), []*workflow.Decision{{
			DecisionType: workflow.DecisionTypePtr(workflow.DecisionType_CompleteWorkflowExecution),
			CompleteWorkflowExecutionDecisionAttributes: &workflow.CompleteWorkflowExecutionDecisionAttributes{
				Result_: []byte("Done."),
			},
		}}
	}

	activityExecutedCount := 0
	atHandler := func(execution *workflow.WorkflowExecution, activityType *workflow.ActivityType,
		activityID string, startedEventID int64, input []byte, taskToken []byte) ([]byte, bool, error) {
		s.Equal(id, execution.GetWorkflowId())
		s.Equal(activityName, activityType.GetName())
		for i := 0; i < 10; i++ {
			s.logger.Infof("Heartbeating for activity: %s, count: %d", activityID, i)
			response, err := s.engine.RecordActivityTaskHeartbeat(&workflow.RecordActivityTaskHeartbeatRequest{
				TaskToken: taskToken, Details: []byte("details")})
			if response.GetCancelRequested() {
				return []byte("Activity Cancelled."), true, nil
			}
			s.Nil(err)
			time.Sleep(10 * time.Millisecond)
		}
		activityExecutedCount++
		return []byte("Activity Result."), false, nil
	}

	poller := &taskPoller{
		engine:          s.engine,
		taskList:        taskList,
		identity:        identity,
		decisionHandler: dtHandler,
		activityHandler: atHandler,
		logger:          s.logger,
	}

	err := poller.pollAndProcessDecisionTask(false, false)
	s.True(err == nil || err == matching.ErrNoTasks)

	cancelCh := make(chan struct{})

	go func() {
		s.logger.Info("Trying to cancel the task in a different thread.")
		scheduleActivity = false
		requestCancellation = true
		err := poller.pollAndProcessDecisionTask(false, false)
		s.True(err == nil || err == matching.ErrNoTasks)
		cancelCh <- struct{}{}
	}()

	err = poller.pollAndProcessActivityTask(false)
	s.True(err == nil || err == matching.ErrNoTasks)

	<-cancelCh
	s.logger.Infof("Waiting for workflow to complete: RunId: %v", we.GetRunId())
}

func (s *integrationSuite) setupShards() {
	// shard 0 is always created, we create additional shards if needed
	for shardID := 1; shardID < testNumberOfHistoryShards; shardID++ {
		err := s.CreateShard(shardID, "", 0)
		if err != nil {
			s.logger.WithField("error", err).Fatal("Failed to create shard")
		}
	}
}
