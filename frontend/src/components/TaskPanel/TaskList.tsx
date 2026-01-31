import * as React from "react";
import { TaskItem } from "./TaskItem";
import type { ExportTask, QueueStatus } from "@/types";

interface TaskListProps {
  tasks: ExportTask[];
  queueStatus: QueueStatus | null;
  onCancel: (id: string) => void;
  onDelete: (id: string) => void;
  onSelect: (task: ExportTask) => void;
  onReorder?: (id: string, newIndex: number) => void;
}

export function TaskList({
  tasks,
  queueStatus,
  onCancel,
  onDelete,
  onSelect,
  onReorder,
}: TaskListProps) {
  const [draggedId, setDraggedId] = React.useState<string | null>(null);
  const [dragOverIndex, setDragOverIndex] = React.useState<number | null>(null);

  const handleDragStart = (e: React.DragEvent, taskId: string) => {
    setDraggedId(taskId);
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", taskId);
  };

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    setDragOverIndex(index);
  };

  const handleDragEnd = () => {
    if (draggedId && dragOverIndex !== null) {
      onReorder?.(draggedId, dragOverIndex);
    }
    setDraggedId(null);
    setDragOverIndex(null);
  };

  // Group tasks by status
  const pendingTasks = tasks.filter(t => t.status === "pending");
  const runningTasks = tasks.filter(t => t.status === "running");
  const completedTasks = tasks.filter(
    t => t.status === "completed" || t.status === "failed" || t.status === "cancelled"
  );

  if (tasks.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
        <div className="text-center">
          <p className="text-sm">No tasks in queue</p>
          <p className="text-xs mt-1">Export imagery to add tasks</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Running Tasks */}
      {runningTasks.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-1">
            Running ({runningTasks.length})
          </div>
          {runningTasks.map((task) => (
            <TaskItem
              key={task.id}
              task={task}
              isCurrentTask={task.id === queueStatus?.currentTaskID}
              onCancel={onCancel}
              onDelete={onDelete}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}

      {/* Pending Tasks (draggable) */}
      {pendingTasks.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-1">
            Pending ({pendingTasks.length})
          </div>
          {pendingTasks.map((task, index) => (
            <div
              key={task.id}
              draggable
              onDragStart={(e) => handleDragStart(e, task.id)}
              onDragOver={(e) => handleDragOver(e, index)}
              onDragEnd={handleDragEnd}
              className={dragOverIndex === index ? "border-t-2 border-blue-500" : ""}
            >
              <TaskItem
                task={task}
                onCancel={onCancel}
                onDelete={onDelete}
                onSelect={onSelect}
                isDragging={draggedId === task.id}
              />
            </div>
          ))}
        </div>
      )}

      {/* Completed Tasks */}
      {completedTasks.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-1">
            Completed ({completedTasks.length})
          </div>
          {completedTasks.map((task) => (
            <TaskItem
              key={task.id}
              task={task}
              onCancel={onCancel}
              onDelete={onDelete}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}
