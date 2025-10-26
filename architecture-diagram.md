# Master-Worker Architecture Diagram

```
                    ┌─────────────────────────────────────────────┐
                    │                Master Node                  │
                    │                                             │
                    │  ┌─────────────┐    ┌────────────────────┐  │
                    │  │ Task Queue  │    │ Active Task Tracker│  │
                    │  │             │    │                    │  │
                    │  │  [Task]     │    │ TaskID → Status    │  │
                    │  │  [Task]     │    │        → WorkerID  │  │
                    │  │  [Task]     │    │        → Start     │  │
                    │  │    ...      │    │        → Attempts  │  │
                    │  └─────────────┘    └────────────────────┘  │
                    │                                             │
                    │  ┌────────────────────────────────────────┐ │
                    │  │         gRPC Server (Port 50051)       │ │
                    │  │                                        │ │
                    │  │  ┌──────────────────────────────────┐  │ │
                    │  │  │        Worker Connections        │  │ │
                    │  │  │                                  │  │ │
                    │  │  │ WorkerID → Stream                │  │ │
                    │  │  │            → Message Channel     │  │ │
                    │  │  │            → Context             │  │ │
                    │  │  └──────────────────────────────────┘  │ │
                    │  └────────────────────────────────────────┘ │
                    └─────────────────────────────────────────────┘
                                      ▲     │
                                      │     │
                             [gRPC Streams] │
                                      │     │ [Task Assignment]
                                      │     ▼
                    ┌─────────────────────────────────────────────┐
                    │               Worker Node 1                 │
                    │                                             │
                    │  ┌────────────────────────────────────────┐ │
                    │  │         gRPC Client Connection         │ │
                    │  │                                        │ │
                    │  │  ┌──────────────────────────────────┐  │ │
                    │  │  │         Message Handler          │  │ │
                    │  │  │                                  │  │ │
                    │  │  │  Receives: Task                  │  │ │
                    │  │  │  Sends: TaskResult               │  │ │
                    │  │  └──────────────────────────────────┘  │ │
                    │  └────────────────────────────────────────┘ │
                    │                                             │
                    │  ┌────────────────────────────────────────┐ │
                    │  │          Task Executor              │ │
                    │  │                                     │ │
                    │  │  Simulates work                     │ │
                    │  │  Reports success/failure            │ │
                    │  │  Handles retries                    │ │
                    │  └────────────────────────────────────────┘ │
                    └─────────────────────────────────────────────┘

                    ┌─────────────────────────────────────────────┐
                    │               Worker Node N                 │
                    │                                             │
                    │  ┌────────────────────────────────────────┐ │
                    │  │         gRPC Client Connection         │ │
                    │  │                                        │ │
                    │  │  ┌──────────────────────────────────┐  │ │
                    │  │  │         Message Handler          │  │ │
                    │  │  │                                  │  │ │
                    │  │  │  Receives: Task                  │  │ │
                    │  │  │  Sends: TaskResult               │  │ │
                    │  │  └──────────────────────────────────┘  │ │
                    │  └────────────────────────────────────────┘ │
                    │                                             │
                    │  ┌────────────────────────────────────────┐ │
                    │  │          Task Executor              │ │
                    │  │                                     │ │
                    │  │  Simulates work                     │ │
                    │  │  Reports success/failure            │ │
                    │  │  Handles retries                    │ │
                    │  └────────────────────────────────────────┘ │
                    └─────────────────────────────────────────────┘
```

## Communication Details

### 1. Connection Establishment
- **Method**: gRPC bidirectional streaming
- **Process**: 
  - Workers initiate connection to master at `localhost:50051`
  - Workers send `WorkerHello` message with their unique ID
  - Master registers worker in its connection map

### 2. Task Distribution
- **Method**: gRPC stream message from master to worker
- **Content**: `MasterMsg` containing a `Task` object
- **Process**:
  - Master periodically generates tasks and adds them to the task queue
  - Dispatch loop picks tasks from queue
  - Master randomly selects an available worker
  - Master sends task via the worker's message channel

### 3. Task Execution
- **Method**: Local processing in worker node
- **Content**: Task with ID, payload, and attempt count
- **Process**:
  - Worker receives task from master stream
  - Worker spawns goroutine to handle task
  - Worker simulates work with time.Sleep
  - Worker determines success/failure based on demo logic

### 4. Result Reporting
- **Method**: gRPC stream message from worker to master
- **Content**: `WorkerMsg` containing a `TaskResult` object
- **Process**:
  - Worker sends result back through the same gRPC stream
  - Master receives result and updates task status
  - Master removes task from active tasks map
  - If failed, master reschedules task (max 3 attempts)

### 5. Timeout Handling
- **Method**: Goroutine monitoring with context cancellation
- **Content**: Task ID and timeout duration (10 seconds)
- **Process**:
  - Master starts timeout watcher for each assigned task
  - If context is cancelled (task completed), watcher exits
  - If timer expires, task is marked as TIMEOUT
  - Master reschedules timed out tasks (max 3 attempts)

### 6. Worker Disconnection
- **Method**: gRPC stream error detection
- **Content**: Connection error or EOF
- **Process**:
  - Master detects worker disconnection through stream errors
  - Master unregisters worker from workers map
  - Master cleans up worker resources
  - Master reschedules any tasks assigned to disconnected worker