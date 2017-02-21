package mocks

import "github.com/uber/cadence/common/persistence"
import "github.com/stretchr/testify/mock"

// TaskManager is an autogenerated mock type for the TaskManager type
type TaskManager struct {
	mock.Mock
}

// LeaseTaskList provides a mock function with given fields: request
func (_m *TaskManager) LeaseTaskList(request *persistence.LeaseTaskListRequest) (*persistence.LeaseTaskListResponse, error) {
	ret := _m.Called(request)

	var r0 *persistence.LeaseTaskListResponse
	var r1 error
	if rf, ok := ret.Get(0).(func(*persistence.LeaseTaskListRequest) (*persistence.LeaseTaskListResponse, error)); ok {
		return rf(request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*persistence.LeaseTaskListResponse)
		}
	}

	if rf, ok := ret.Get(1).(func(*persistence.LeaseTaskListRequest) error); ok {
		r1 = rf(request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// UpdateTaskList provides a mock function with given fields: request
func (_m *TaskManager) UpdateTaskList(request *persistence.UpdateTaskListRequest) (*persistence.UpdateTaskListResponse, error) {
	ret := _m.Called(request)

	var r0 *persistence.UpdateTaskListResponse
	if rf, ok := ret.Get(0).(func(*persistence.UpdateTaskListRequest) *persistence.UpdateTaskListResponse); ok {
		r0 = rf(request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*persistence.UpdateTaskListResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(*persistence.UpdateTaskListRequest) error); ok {
		r1 = rf(request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// CompleteTask provides a mock function with given fields: request
func (_m *TaskManager) CompleteTask(request *persistence.CompleteTaskRequest) error {
	ret := _m.Called(request)

	var r0 error
	if rf, ok := ret.Get(0).(func(*persistence.CompleteTaskRequest) error); ok {
		r0 = rf(request)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CreateTask provides a mock function with given fields: request
func (_m *TaskManager) CreateTask(request *persistence.CreateTaskRequest) (*persistence.CreateTaskResponse, error) {
	ret := _m.Called(request)

	var r0 *persistence.CreateTaskResponse
	if rf, ok := ret.Get(0).(func(*persistence.CreateTaskRequest) *persistence.CreateTaskResponse); ok {
		r0 = rf(request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*persistence.CreateTaskResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(*persistence.CreateTaskRequest) error); ok {
		r1 = rf(request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetTasks provides a mock function with given fields: request
func (_m *TaskManager) GetTasks(request *persistence.GetTasksRequest) (*persistence.GetTasksResponse, error) {
	ret := _m.Called(request)

	var r0 *persistence.GetTasksResponse
	if rf, ok := ret.Get(0).(func(*persistence.GetTasksRequest) (*persistence.GetTasksResponse, error)); ok {
		return rf(request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*persistence.GetTasksResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(*persistence.GetTasksRequest) error); ok {
		r1 = rf(request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
