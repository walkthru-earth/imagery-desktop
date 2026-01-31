import * as React from "react";
import { useState, useEffect, useCallback } from "react";
import {
  Play,
  Pause,
  PanelRightClose,
  PanelRight,
  Trash2,
  RefreshCw,
  Plus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { TaskList } from "./TaskList";
import { api } from "@/services/api";
import type { ExportTask, QueueStatus, TaskProgress } from "@/types";
import { cn } from "@/lib/utils";

interface TaskPanelProps {
  isOpen: boolean;
  onToggle: () => void;
  onTaskSelect?: (task: ExportTask) => void;
  onAddTask?: () => void;
}

export function TaskPanel({ isOpen, onToggle, onTaskSelect, onAddTask }: TaskPanelProps) {
  const [tasks, setTasks] = useState<ExportTask[]>([]);
  const [queueStatus, setQueueStatus] = useState<QueueStatus | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Load tasks on mount
  const loadTasks = useCallback(async () => {
    try {
      const [taskList, status] = await Promise.all([
        api.getTaskQueue(),
        api.getTaskQueueStatus(),
      ]);
      // Convert Wails types to local types (status string to union type)
      const convertedTasks: ExportTask[] = (taskList || []).map((t: any) => ({
        ...t,
        status: t.status as ExportTask["status"],
      }));
      setTasks(convertedTasks);
      setQueueStatus(status as QueueStatus);
    } catch (error) {
      console.error("Failed to load tasks:", error);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadTasks();

    // Subscribe to task queue events
    const unsubQueueUpdate = api.onTaskQueueUpdate((status: QueueStatus) => {
      setQueueStatus(status);
      loadTasks(); // Reload tasks when queue status changes
    });

    const unsubTaskProgress = api.onTaskProgress(
      (event: { taskId: string; progress: TaskProgress }) => {
        setTasks((prevTasks) =>
          prevTasks.map((task) =>
            task.id === event.taskId
              ? { ...task, progress: event.progress }
              : task
          )
        );
      }
    );

    const unsubTaskComplete = api.onTaskComplete(
      (event: { taskId: string; success: boolean; error?: string }) => {
        loadTasks(); // Reload to get updated task status
      }
    );

    const unsubNotification = api.onSystemNotification(
      (notification: { title: string; message: string; type: string }) => {
        // Show browser notification if permission granted
        if (Notification.permission === "granted") {
          new Notification(notification.title, { body: notification.message });
        }
      }
    );

    // Request notification permission
    if (Notification.permission === "default") {
      Notification.requestPermission();
    }

    return () => {
      // Cleanup event listeners (Wails doesn't expose EventsOff directly)
    };
  }, [loadTasks]);

  const handleStartQueue = async () => {
    try {
      await api.startTaskQueue();
    } catch (error) {
      console.error("Failed to start queue:", error);
    }
  };

  const handlePauseQueue = async () => {
    try {
      await api.pauseTaskQueue();
    } catch (error) {
      console.error("Failed to pause queue:", error);
    }
  };

  const handleCancelTask = async (id: string) => {
    try {
      await api.cancelTask(id);
      loadTasks();
    } catch (error) {
      console.error("Failed to cancel task:", error);
    }
  };

  const handleDeleteTask = async (id: string) => {
    console.log("[TaskPanel] Deleting task:", id);
    try {
      await api.deleteTask(id);
      console.log("[TaskPanel] Task deleted successfully");
      loadTasks();
    } catch (error) {
      console.error("[TaskPanel] Failed to delete task:", error);
      // Show error to user
      alert("Failed to delete task: " + error);
    }
  };

  const handleReorderTask = async (id: string, newIndex: number) => {
    try {
      await api.reorderTask(id, newIndex);
      loadTasks();
    } catch (error) {
      console.error("Failed to reorder task:", error);
    }
  };

  const handleClearCompleted = async () => {
    try {
      await api.clearCompletedTasks();
      loadTasks();
    } catch (error) {
      console.error("Failed to clear completed tasks:", error);
    }
  };

  const pendingCount = tasks.filter(
    (t) => t.status === "pending" || t.status === "running"
  ).length;
  const completedCount = tasks.filter(
    (t) =>
      t.status === "completed" ||
      t.status === "failed" ||
      t.status === "cancelled"
  ).length;

  // Collapsed view - just show toggle button
  if (!isOpen) {
    return (
      <Button
        variant="outline"
        size="sm"
        className="fixed right-4 top-16 z-50 gap-2 shadow-md"
        onClick={onToggle}
      >
        <PanelRight className="w-4 h-4" />
        <span>Tasks</span>
        {pendingCount > 0 && (
          <span className="inline-flex items-center justify-center w-5 h-5 text-xs font-medium bg-blue-500 text-white rounded-full">
            {pendingCount}
          </span>
        )}
      </Button>
    );
  }

  return (
    <div
      className={cn(
        "fixed right-0 top-12 bottom-0 z-40",
        "w-80 bg-background border-l shadow-lg",
        "flex flex-col transition-transform duration-200",
        isOpen ? "translate-x-0" : "translate-x-full"
      )}
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold">Export Queue</h2>
          {pendingCount > 0 && (
            <span className="inline-flex items-center justify-center px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300 rounded-full">
              {pendingCount} pending
            </span>
          )}
        </div>
        <Button variant="ghost" size="icon" onClick={onToggle}>
          <PanelRightClose className="w-4 h-4" />
        </Button>
      </div>

      {/* Queue Controls */}
      <div className="flex flex-col gap-2 p-4 border-b bg-muted/30">
        {/* Add Task Button */}
        {onAddTask && (
          <Button
            variant="secondary"
            size="sm"
            onClick={onAddTask}
            className="w-full"
          >
            <Plus className="w-4 h-4 mr-2" />
            Add Task
          </Button>
        )}

        {/* Play/Pause Controls */}
        <div className="flex items-center gap-2">
          {queueStatus?.isRunning && !queueStatus?.isPaused ? (
            <Button
              variant="outline"
              size="sm"
              onClick={handlePauseQueue}
              className="flex-1"
            >
              <Pause className="w-4 h-4 mr-2" />
              Pause Queue
            </Button>
          ) : (
            <Button
              variant="default"
              size="sm"
              onClick={handleStartQueue}
              disabled={pendingCount === 0}
              className="flex-1"
            >
              <Play className="w-4 h-4 mr-2" />
              {queueStatus?.isPaused ? "Resume Queue" : "Start Queue"}
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon"
            onClick={loadTasks}
            title="Refresh"
          >
            <RefreshCw className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Task List */}
      <div className="flex-1 overflow-y-auto p-4">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <RefreshCw className="w-5 h-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <TaskList
            tasks={tasks}
            queueStatus={queueStatus}
            onCancel={handleCancelTask}
            onDelete={handleDeleteTask}
            onSelect={(task) => onTaskSelect?.(task)}
            onReorder={handleReorderTask}
          />
        )}
      </div>

      {/* Footer */}
      {completedCount > 0 && (
        <div className="p-4 border-t bg-muted/30">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleClearCompleted}
            className="w-full text-muted-foreground"
          >
            <Trash2 className="w-4 h-4 mr-2" />
            Clear {completedCount} completed
          </Button>
        </div>
      )}
    </div>
  );
}
