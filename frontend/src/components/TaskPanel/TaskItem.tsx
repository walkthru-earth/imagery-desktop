import * as React from "react";
import { Play, Pause, Trash2, GripVertical, CheckCircle, XCircle, Loader2, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ExportTask, TaskStatus } from "@/types";
import { cn } from "@/lib/utils";

interface TaskItemProps {
  task: ExportTask;
  isCurrentTask?: boolean;
  onCancel?: (id: string) => void;
  onDelete?: (id: string) => void;
  onSelect?: (task: ExportTask) => void;
  isDragging?: boolean;
}

const statusIcons: Record<TaskStatus, React.ReactNode> = {
  pending: <Clock className="w-4 h-4 text-muted-foreground" />,
  running: <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />,
  failed: <XCircle className="w-4 h-4 text-red-500" />,
  cancelled: <XCircle className="w-4 h-4 text-muted-foreground" />,
};

const statusLabels: Record<TaskStatus, string> = {
  pending: "Pending",
  running: "Running",
  completed: "Completed",
  failed: "Failed",
  cancelled: "Cancelled",
};

export function TaskItem({
  task,
  isCurrentTask,
  onCancel,
  onDelete,
  onSelect,
  isDragging,
}: TaskItemProps) {
  const canDelete = task.status !== "running";
  const canCancel = task.status === "running" || task.status === "pending";

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return "";
    const date = new Date(dateStr);
    return date.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  };

  return (
    <div
      className={cn(
        "group flex items-center gap-2 p-3 rounded-lg border transition-all",
        isDragging && "opacity-50",
        isCurrentTask && "border-blue-500 bg-blue-50 dark:bg-blue-950",
        task.status === "completed" && "opacity-60",
        task.status === "failed" && "border-red-200 dark:border-red-900",
        !isDragging && !isCurrentTask && "hover:bg-muted/50 cursor-pointer"
      )}
      onClick={() => onSelect?.(task)}
    >
      {/* Drag Handle */}
      <div className="cursor-grab opacity-0 group-hover:opacity-50 transition-opacity">
        <GripVertical className="w-4 h-4" />
      </div>

      {/* Status Icon */}
      <div className="flex-shrink-0">{statusIcons[task.status]}</div>

      {/* Task Info */}
      <div className="flex-1 min-w-0">
        <div className="font-medium text-sm truncate">{task.name}</div>
        <div className="text-xs text-muted-foreground flex items-center gap-2">
          <span className="capitalize">{task.source}</span>
          <span>Z{task.zoom}</span>
          <span>{task.dates.length} dates</span>
        </div>

        {/* Progress bar for running tasks */}
        {task.status === "running" && (
          <div className="mt-1">
            <div className="flex items-center justify-between text-xs text-muted-foreground mb-1">
              <span>{task.progress.currentPhase || "Processing"}</span>
              <span>{task.progress.percent}%</span>
            </div>
            <div className="h-1.5 bg-muted rounded-full overflow-hidden">
              <div
                className="h-full bg-blue-500 transition-all duration-300"
                style={{ width: `${task.progress.percent}%` }}
              />
            </div>
          </div>
        )}

        {/* Error message for failed tasks */}
        {task.status === "failed" && task.error && (
          <div className="mt-1 text-xs text-red-500 truncate" title={task.error}>
            {task.error}
          </div>
        )}

        {/* Completion time for completed tasks */}
        {task.status === "completed" && task.completedAt && (
          <div className="text-xs text-muted-foreground mt-0.5">
            Completed {formatDate(task.completedAt)}
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
        {canCancel && (
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={(e) => {
              e.stopPropagation();
              onCancel?.(task.id);
            }}
            title="Cancel task"
          >
            <Pause className="w-3.5 h-3.5" />
          </Button>
        )}
        {canDelete && (
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-red-500 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-950"
            onClick={(e) => {
              e.stopPropagation();
              onDelete?.(task.id);
            }}
            title="Delete task"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </Button>
        )}
      </div>
    </div>
  );
}
