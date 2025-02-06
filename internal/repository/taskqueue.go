package repository

import (
	"context"
	_ "embed"
	"errors"
	"github.com/TicketsBot/export/internal/model"
	"github.com/TicketsBot/export/internal/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TaskRepository struct {
	tx pgx.Tx
}

var (
	//go:embed sql/task_queue/create.sql
	queryTaskQueueCreate string

	//go:embed sql/task_queue/get_next.sql
	queryTaskQueueGetNext string

	//go:embed sql/task_queue/delete.sql
	queryTaskQueueDelete string
)

func NewTaskRepository(tx pgx.Tx) *TaskRepository {
	return &TaskRepository{
		tx: tx,
	}
}

func (r *TaskRepository) Create(ctx context.Context, requestId uuid.UUID) (uuid.UUID, error) {
	var taskId uuid.UUID
	if err := r.tx.QueryRow(ctx, queryTaskQueueCreate, requestId).Scan(&taskId); err != nil {
		return uuid.Nil, err
	}

	return taskId, nil
}

func (r *TaskRepository) GetNext(ctx context.Context) (*model.Union[model.Task, model.Request], error) {
	var task model.Task
	var request model.Request

	if err := r.tx.QueryRow(ctx, queryTaskQueueGetNext).Scan(
		&task.Id,
		&task.RequestId,
		&request.Id,
		&request.UserId,
		&request.Type,
		&request.CreatedAt,
		&request.GuildId,
		&request.Status,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	return utils.Ptr(model.NewUnion(task, request)), nil
}

func (r *TaskRepository) Delete(ctx context.Context, taskId uuid.UUID) error {
	_, err := r.tx.Exec(ctx, queryTaskQueueDelete, taskId)
	return err
}
