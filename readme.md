# Go Distributed Master-Worker Demo

This is a distributed task processing system implemented in Go using gRPC for communication between a master node and multiple worker nodes. The system demonstrates a master-worker architecture where the master distributes tasks to connected workers, tracks their execution status, handles failures, and implements timeout mechanisms.

## Architecture Overview

The system consists of:
- **Master**: Central coordinator that manages tasks and worker connections
- **Workers**: Execute tasks assigned by the master
- **gRPC Communication**: Bidirectional streaming for real-time communication

## Key Features

1. **Task Distribution**: Master distributes tasks to available workers
2. **Task Status Tracking**: Master maintains status of all active tasks
3. **Failure Handling**: Automatic rescheduling of failed tasks (up to 3 attempts)
4. **Timeout Management**: Tasks that exceed execution time are timed out and rescheduled
5. **Worker Management**: Dynamic registration and deregistration of workers
6. **Load Distribution**: Simple random worker selection for task assignment

## Project Structure

```
.
├── go.mod
├── go.sum
├── main.go              # Master implementation
├── proto/
│   ├── task.proto       # Protocol buffer definitions
│   └── learn-master-slave/
│       └── taskpb/      # Generated protobuf code
├── readme.md            # This file
└── worker/
    └── main.go          # Worker implementation
```

## Protocol Design

The communication between master and workers is defined in `task.proto`:

- **Task**: Represents a unit of work with ID, payload, and attempt count
- **TaskResult**: Contains the result of task execution (success/failure)
- **WorkerHello**: Worker registration message with ID
- **MasterMsg**: Messages from master to worker (tasks, ping)
- **WorkerMsg**: Messages from worker to master (hello, results)
- **Master Service**: Defines the bidirectional streaming RPC

## Master Implementation Details

The master maintains several key data structures:
- `workers`: Map of connected workers
- `tasks`: Map of active tasks with their status and metadata
- `taskQueue`: Channel for pending tasks

### Key Components

1. **WorkerStream**: Handles bidirectional communication with workers
2. **dispatchLoop**: Continuously assigns tasks from queue to available workers
3. **assignTask**: Selects a worker and sends task for execution
4. **handleResult**: Processes results from workers
5. **waitTaskTimeout**: Monitors tasks for timeout conditions

## Worker Implementation Details

Workers connect to the master and:
1. Register with a unique ID
2. Listen for tasks from the master
3. Execute tasks with simulated work duration
4. Report results back to the master

## How to Run

1. **Generate protobuf code**:
   ```bash
   protoc --go_out=. --go-grpc_out=. proto/task.proto
   ```

2. **Start the master**:
   ```bash
   go run main.go
   ```
   By default, the master listens on port 50051.

3. **Start workers**:
   ```bash
   go run worker/main.go --id=worker-1 --master=localhost:50051
   go run worker/main.go --id=worker-2 --master=localhost:50051
   ```
   You can start multiple workers with different IDs.

## Interaction Flow

See `interaction-diagram.txt` for a detailed diagram of the communication between master and workers.

## Demo Behavior

- The master generates tasks periodically (every 2 seconds)
- Workers simulate task execution with a delay
- Workers randomly fail tasks for demonstration purposes
- Failed tasks are retried up to 3 times
- Tasks that exceed 10 seconds are timed out and rescheduled

## Possible Improvements

1. **Smart Scheduling**: Replace random worker selection with load-based scheduling
2. **Persistence**: Store task state in a database for crash recovery
3. **Security**: Use TLS for gRPC communication
4. **High Availability**: Implement master leader election for fault tolerance
5. **Advanced Retry Logic**: Implement exponential backoff for task retries

## Dependencies

- Go 1.16+
- gRPC-Go
- Protocol Buffers

## License

This project is provided as a learning example and is free to use and modify.