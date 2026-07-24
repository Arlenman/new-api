/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TFunction } from "i18next";
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  ListChecks,
  LoaderCircle,
  RefreshCw,
  Trash2,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { copyToClipboard } from "@/lib/copy-to-clipboard";
import { formatTimestampToDate } from "@/lib/format";

import {
  clearManagedUpstreamPriorityTasks,
  getManagedUpstreamPrioritySchedule,
  getManagedUpstreamPriorityTasks,
  runManagedUpstreamPrioritySchedule,
  updateManagedUpstreamPrioritySchedule,
} from "../api";
import type {
  UpstreamPriorityTaskAction,
  UpstreamPriorityTaskIssue,
  UpstreamPriorityTaskRecord,
  UpstreamPriorityTaskResult,
  UpstreamPriorityTaskStatus,
  UpstreamPriorityTaskTrigger,
} from "../types";

const DEFAULT_TASK_PAGE_SIZE = 10;
const TASK_PAGE_SIZE_OPTIONS = [10, 20, 50];
const ACTIVE_TASK_POLL_INTERVAL_MS = 2500;
const IDLE_TASK_POLL_INTERVAL_MS = 10000;

const TASK_STATUS_CLASS_NAME: Record<UpstreamPriorityTaskStatus, string> = {
  pending:
    "bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
  running: "bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
  succeeded:
    "bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
  failed: "",
};

const TASK_TRIGGER_LABEL: Record<UpstreamPriorityTaskTrigger, string> = {
  scheduled: "Scheduled",
  manual: "Manual",
};

const GENERIC_HTML_ERROR_MESSAGE = "Upstream returned an HTML error page";

function isActiveTask(status: UpstreamPriorityTaskStatus) {
  return status === "pending" || status === "running";
}

function formatTaskDuration(
  durationMs: number | null | undefined,
  t: TFunction,
) {
  if (durationMs == null || durationMs < 0) return "-";
  if (durationMs < 1000) {
    return t("{{count}} ms", { count: Math.round(durationMs) });
  }
  return t("{{count}} s", {
    count: Number((durationMs / 1000).toFixed(2)),
  });
}

function formatTaskResult(
  result: UpstreamPriorityTaskResult | null | undefined,
  t: TFunction,
) {
  if (!result) return t("No changes");

  const metrics = [
    t("Refreshed {{count}}", { count: result.refreshed ?? 0 }),
    t("Ranked {{count}}", { count: result.ranked ?? 0 }),
    t("Tested {{count}}", { count: result.tested ?? 0 }),
    t("Passed {{count}}", { count: result.passed ?? 0 }),
    t("Priorities updated {{count}}", {
      count: result.priority_updated ?? 0,
    }),
    t("Skipped {{count}}", { count: result.skipped ?? 0 }),
  ];

  const hasChanges = [
    result.refreshed,
    result.ranked,
    result.tested,
    result.passed,
    result.priority_updated,
    result.skipped,
  ].some((count) => (count ?? 0) > 0);

  return hasChanges ? metrics.join(", ") : t("No changes");
}

function getRequestErrorMessage(error: unknown, fallback: string) {
  const responseError = error as {
    message?: string;
    response?: { data?: { message?: string } };
  };

  return (
    responseError.response?.data?.message || responseError.message || fallback
  );
}

function collapseTaskIssueMessage(message: string | undefined) {
  const compactMessage = message?.replaceAll(/\s+/g, " ").trim();
  if (!compactMessage) return undefined;
  if (!/<(?:!doctype|html|head|body|center|h1|title)\b/i.test(compactMessage)) {
    return compactMessage;
  }

  const context = compactMessage.match(
    /^(upstream channel \d+ [^:]{1,100})/i,
  )?.[1];
  const upstreamStatus = compactMessage.match(
    /upstream returned HTTP\s+(\d{3})(?:\s*:\s*([^<]{1,80}))?/i,
  );
  const headingStatus = compactMessage.match(
    /<(?:h1|title)[^>]*>\s*(\d{3})\s*([^<]*)/i,
  );
  const statusCode = upstreamStatus?.[1] || headingStatus?.[1];
  const statusReason = (upstreamStatus?.[2] || headingStatus?.[2] || "")
    .replaceAll(/\s+/g, " ")
    .trim();
  const status = statusCode
    ? `HTTP ${statusCode}${statusReason ? ` ${statusReason}` : ""}`
    : "";

  return (
    [context, status].filter(Boolean).join(" · ") || GENERIC_HTML_ERROR_MESSAGE
  );
}

function getTaskIssues(task: UpstreamPriorityTaskRecord) {
  const rawIssues: Array<string | UpstreamPriorityTaskIssue> = [];
  if (task.error?.trim()) rawIssues.push(task.error);
  if (Array.isArray(task.result?.issues) && task.result.issues.length > 0) {
    rawIssues.push(...task.result.issues);
  } else if (Array.isArray(task.result?.errors)) {
    rawIssues.push(...task.result.errors);
  }

  const seen = new Set<string>();
  const issues: UpstreamPriorityTaskIssue[] = [];
  for (const rawIssue of rawIssues) {
    const issue =
      typeof rawIssue === "string"
        ? { message: collapseTaskIssueMessage(rawIssue) }
        : {
            channel_id: rawIssue.channel_id,
            channel_name: rawIssue.channel_name?.trim(),
            provider: rawIssue.provider?.trim(),
            host: rawIssue.host?.trim(),
            stage: rawIssue.stage?.trim(),
            http_status: rawIssue.http_status,
            message: collapseTaskIssueMessage(rawIssue.message),
          };
    const issueKey = [
      issue.channel_id,
      issue.channel_name,
      issue.provider,
      issue.host,
      issue.stage,
      issue.http_status,
      issue.message,
    ].join("|");
    if (!issueKey.replaceAll("|", "") || seen.has(issueKey)) continue;
    seen.add(issueKey);
    issues.push(issue);
  }
  return issues;
}

function getTaskIssueStage(stage: string | undefined, t: TFunction) {
  switch (stage) {
    case "refresh_groups":
      return t("Refresh groups and multipliers");
    case "prepare_candidate":
      return t("Prepare priority candidate");
    case "reveal_key":
      return t("Reveal API key");
    case "test_channel":
      return t("Test Channel Connection");
    default:
      return stage;
  }
}

function getTaskIssueMessage(message: string | undefined, t: TFunction) {
  if (message === GENERIC_HTML_ERROR_MESSAGE) {
    return t("Upstream returned an HTML error page");
  }
  return message;
}

function getTaskIssueReason(issue: UpstreamPriorityTaskIssue, t: TFunction) {
  const reason =
    getTaskIssueMessage(issue.message, t) ||
    [
      issue.channel_name,
      issue.provider,
      issue.host,
      getTaskIssueStage(issue.stage, t),
    ]
      .filter(Boolean)
      .join(" · ") ||
    t("Unknown");
  const singleLineReason = reason.replaceAll(/\s+/g, " ").trim();
  return singleLineReason.length > 120
    ? `${singleLineReason.slice(0, 117)}...`
    : singleLineReason;
}

interface TaskIssueDetailsProps {
  index: number;
  issue: UpstreamPriorityTaskIssue;
}

function TaskIssueDetails({ index, issue }: TaskIssueDetailsProps) {
  const { t } = useTranslation();
  const fields = [
    { label: t("Channel ID"), value: issue.channel_id },
    { label: t("Channel name"), value: issue.channel_name },
    { label: t("Provider"), value: issue.provider },
    { label: t("Host"), value: issue.host },
    { label: t("Stage"), value: getTaskIssueStage(issue.stage, t) },
    { label: t("HTTP status"), value: issue.http_status },
    { label: t("Message"), value: getTaskIssueMessage(issue.message, t) },
  ].filter(
    ({ value }) => value !== undefined && value !== null && value !== "",
  );

  return (
    <div className="bg-muted/35 rounded-md border p-2 text-xs">
      <div className="mb-1.5 font-medium">
        {t("Issue {{count}}", { count: index + 1 })}
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-1">
        {fields.map(({ label, value }) => (
          <div className="contents" key={label}>
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words">{String(value)}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function PriorityScheduleLogicExplanation() {
  const { t } = useTranslation();
  const steps = [
    t(
      "Refresh each configured upstream channel to obtain its latest groups, models, group ratios, and available keys.",
    ),
    t(
      "Calculate the effective multiplier from the selected group ratio and channel multiplier, then rank lower-cost channels ahead of higher-cost channels.",
    ),
    t(
      "Test each ranked channel with its saved default test model and require the request to finish within the configured maximum latency.",
    ),
    t(
      "Only after a test passes, synchronize the calculated priority to ordinary channels that match the same base URL and selected upstream keys.",
    ),
    t(
      "Skip channels with incomplete credentials, unavailable groups or models, unmatched ordinary channels, failed tests, or excessive latency, without changing their ordinary-channel priority.",
    ),
  ];

  return (
    <div className="bg-muted/35 space-y-2 rounded-md border p-3 text-sm">
      <div className="font-medium">{t("What this task does")}</div>
      <ol className="text-muted-foreground list-decimal space-y-1 pl-5">
        {steps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      <div className="border-t pt-2">
        <span className="font-medium">{t("Why this is necessary")}: </span>
        <span className="text-muted-foreground">
          {t(
            "Upstream prices, available groups, keys, and connectivity can change over time. Re-ranking keeps lower-cost usable channels preferred, while the test-first rule prevents an unverified or slow channel from receiving production traffic only because it is cheaper.",
          )}
        </span>
      </div>
    </div>
  );
}

function getTaskActionTitle(action: UpstreamPriorityTaskAction, t: TFunction) {
  return (
    action.channel_name ||
    action.target_channel_name ||
    (action.channel_id
      ? t("Upstream channel {{id}}", { id: action.channel_id })
      : t("Unknown channel"))
  );
}

interface TaskActionDetailsProps {
  action: UpstreamPriorityTaskAction;
}

function TaskActionDetails(props: TaskActionDetailsProps) {
  const { t } = useTranslation();
  const action = props.action;
  let testResult: string | undefined;
  if (action.passed !== undefined) {
    testResult = action.passed ? t("Passed") : t("Failed");
  }
  const fields = [
    { label: t("Upstream channel"), value: action.channel_name },
    { label: t("Upstream channel ID"), value: action.channel_id },
    { label: t("Domain"), value: action.host },
    { label: t("Provider"), value: action.provider },
    { label: t("Ordinary channel"), value: action.target_channel_name },
    { label: t("Ordinary channel ID"), value: action.target_channel_id },
    { label: t("Ordinary channel domain"), value: action.target_channel_host },
    {
      label: t("Ordinary channel provider"),
      value: action.target_channel_provider,
    },
    { label: t("Test model"), value: action.model },
    { label: t("Effective multiplier"), value: action.effective_ratio },
    { label: t("Original priority"), value: action.old_priority },
    { label: t("New priority"), value: action.new_priority },
    {
      label: t("Test latency"),
      value:
        action.latency_ms === undefined
          ? undefined
          : t("{{count}} ms", { count: action.latency_ms }),
    },
    { label: t("Test result"), value: testResult },
    {
      label: t("Adjustment description"),
      value: action.message ? t(action.message) : undefined,
    },
  ].filter(
    ({ value }) => value !== undefined && value !== null && value !== "",
  );

  return (
    <div className="rounded-md border p-3">
      <div className="mb-2 font-medium">{getTaskActionTitle(action, t)}</div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-sm">
        {fields.map(({ label, value }) => (
          <div className="contents" key={label}>
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words">{String(value)}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

interface TaskResultSummaryProps {
  task: UpstreamPriorityTaskRecord;
}

function TaskResultSummary({ task }: TaskResultSummaryProps) {
  const { t } = useTranslation();
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [selectedMetric, setSelectedMetric] = useState<string | null>(null);
  const result = task.result;
  const issues = getTaskIssues(task);
  const metrics = result
    ? [
        {
          key: "refreshed",
          count: result.refreshed ?? 0,
          label: t("Refreshed {{count}}", { count: result.refreshed ?? 0 }),
        },
        {
          key: "ranked",
          count: result.ranked ?? 0,
          label: t("Ranked {{count}}", { count: result.ranked ?? 0 }),
        },
        {
          key: "tested",
          count: result.tested ?? 0,
          label: t("Tested {{count}}", { count: result.tested ?? 0 }),
        },
        {
          key: "passed",
          count: result.passed ?? 0,
          label: t("Passed {{count}}", { count: result.passed ?? 0 }),
        },
        {
          key: "priority_updated",
          count: result.priority_updated ?? 0,
          label: t("Priorities updated {{count}}", {
            count: result.priority_updated ?? 0,
          }),
        },
        {
          key: "skipped",
          count: result.skipped ?? 0,
          label: t("Skipped {{count}}", { count: result.skipped ?? 0 }),
        },
      ].filter(({ count }) => count > 0)
    : [];
  const selectedMetricLabel = metrics.find(
    (metric) => metric.key === selectedMetric,
  )?.label;
  const selectedActions = (result?.actions ?? []).filter((action) => {
    if (selectedMetric === "passed") {
      return action.kind === "tested" && action.passed === true;
    }
    return action.kind === selectedMetric;
  });

  if (!result && issues.length === 0) return <span>-</span>;

  return (
    <>
      <div className="min-w-0 space-y-1.5 text-xs">
        {result && (
          <div className="flex flex-wrap gap-1">
            {metrics.length > 0 ? (
              metrics.map(({ key, label }) => (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  className="h-5 max-w-full rounded-full px-2 font-normal"
                  key={key}
                  onClick={() => setSelectedMetric(key)}
                >
                  <span className="truncate">{label}</span>
                </Button>
              ))
            ) : (
              <span className="text-muted-foreground">{t("No changes")}</span>
            )}
          </div>
        )}

        {issues.length > 0 && (
          <div className="space-y-1.5">
            <div className="text-destructive break-words">
              {t("{{count}} issues: {{reason}}", {
                count: issues.length,
                reason: getTaskIssueReason(issues[0], t),
              })}
            </div>
            <Button
              type="button"
              variant="link"
              size="xs"
              className="h-auto px-0 py-0 text-xs"
              aria-expanded={detailsOpen}
              onClick={() => setDetailsOpen((open) => !open)}
            >
              {t(detailsOpen ? "Collapse" : "View details")}
            </Button>
            {detailsOpen && (
              <div className="space-y-1.5">
                {issues.map((issue, index) => (
                  <TaskIssueDetails
                    index={index}
                    issue={issue}
                    key={[
                      task.task_id,
                      issue.channel_id,
                      issue.channel_name,
                      issue.provider,
                      issue.host,
                      issue.stage,
                      issue.http_status,
                      issue.message,
                    ].join("|")}
                  />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
      <Dialog
        open={selectedMetric !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedMetric(null);
        }}
      >
        <DialogContent className="max-h-[82vh] sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              {selectedMetricLabel || t("Task details")}
            </DialogTitle>
            <DialogDescription>
              {t(
                "The following records show the exact upstream or ordinary channels affected by this step and what was read, tested, skipped, or changed.",
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
            {selectedActions.length > 0 ? (
              selectedActions.map((action) => (
                <TaskActionDetails
                  action={action}
                  key={[
                    action.kind,
                    action.channel_id,
                    action.target_channel_id,
                    action.target_channel_host,
                    action.target_channel_provider,
                    action.model,
                    action.effective_ratio,
                    action.old_priority,
                    action.new_priority,
                    action.latency_ms,
                    action.message,
                  ].join("|")}
                />
              ))
            ) : (
              <div className="text-muted-foreground rounded-md border p-4 text-sm">
                {t(
                  "This historical task only stored aggregate counts, so channel-level details are unavailable. New task executions will record these details.",
                )}
              </div>
            )}
          </div>
          <DialogFooter>
            <Button type="button" onClick={() => setSelectedMetric(null)}>
              {t("Close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

interface PriorityTaskListDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function PriorityTaskListDialog(props: PriorityTaskListDialogProps) {
  const { t } = useTranslation();
  const [tasks, setTasks] = useState<UpstreamPriorityTaskRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [errorMessage, setErrorMessage] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_TASK_PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const requestInFlightRef = useRef(false);

  const loadTasks = useCallback(
    async (mode: "initial" | "manual" | "poll" = "poll") => {
      if (requestInFlightRef.current) return;

      requestInFlightRef.current = true;
      if (mode === "initial") {
        setLoading(true);
      } else if (mode === "manual") {
        setRefreshing(true);
      }
      if (mode !== "poll") setErrorMessage("");

      try {
        const response = await getManagedUpstreamPriorityTasks(page, pageSize);
        if (!response.success || !response.data?.items) {
          setErrorMessage(
            response.message || t("Failed to load priority task records"),
          );
          return;
        }
        setTasks(response.data.items);
        setTotal(response.data.total);
        if (response.data.page !== page) setPage(response.data.page);
        setErrorMessage("");
      } catch (error) {
        setErrorMessage(
          getRequestErrorMessage(
            error,
            t("Failed to load priority task records"),
          ),
        );
      } finally {
        requestInFlightRef.current = false;
        setLoading(false);
        setRefreshing(false);
      }
    },
    [page, pageSize, t],
  );

  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const hasActiveTasks = tasks.some((task) => isActiveTask(task.status));

  useEffect(() => {
    if (!props.open) return;
    void loadTasks("initial");
  }, [loadTasks, props.open]);

  useEffect(() => {
    if (!props.open) return;

    const intervalId = window.setInterval(
      () => {
        void loadTasks();
      },
      hasActiveTasks
        ? ACTIVE_TASK_POLL_INTERVAL_MS
        : IDLE_TASK_POLL_INTERVAL_MS,
    );

    return () => window.clearInterval(intervalId);
  }, [hasActiveTasks, loadTasks, props.open]);

  async function handleCopyTaskId(taskId: string) {
    const copied = await copyToClipboard(taskId);
    if (copied) {
      toast.success(t("Task ID copied"));
      return;
    }
    toast.error(t("Failed to copy task ID"));
  }

  async function handleClearTasks() {
    if (clearing || hasActiveTasks) return;
    setClearing(true);
    try {
      const response = await clearManagedUpstreamPriorityTasks();
      if (!response.success) {
        toast.error(
          response.message || t("Failed to clear priority task records"),
        );
        return;
      }
      toast.success(
        t("Cleared {{count}} priority task records", {
          count: response.data?.deleted_count ?? 0,
        }),
      );
      setPage(1);
      if (page === 1) await loadTasks("manual");
    } catch (error) {
      toast.error(
        getRequestErrorMessage(
          error,
          t("Failed to clear priority task records"),
        ),
      );
    } finally {
      setClearing(false);
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="max-h-[88vh] grid-rows-[auto_auto_minmax(0,1fr)_auto] sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle>{t("Priority task records")}</DialogTitle>
          <DialogDescription>
            {t(
              "View scheduled and manual priority task execution records. Active tasks refresh automatically.",
            )}
          </DialogDescription>
        </DialogHeader>
        <PriorityScheduleLogicExplanation />

        <div className="min-h-0 space-y-3 overflow-hidden">
          {errorMessage && (
            <Alert variant="destructive">
              <AlertDescription>{errorMessage}</AlertDescription>
            </Alert>
          )}

          {loading ? (
            <div className="text-muted-foreground flex min-h-48 items-center justify-center gap-2 rounded-md border">
              <LoaderCircle className="size-4 animate-spin" />
              {t("Loading...")}
            </div>
          ) : (
            <div className="h-full max-h-[62vh] overflow-x-hidden overflow-y-auto rounded-md border">
              <Table className="table-fixed">
                <TableHeader className="bg-background sticky top-0 z-10">
                  <TableRow className="bg-muted/40 hover:bg-muted/40">
                    <TableHead className="w-[27%]">{t("Task")}</TableHead>
                    <TableHead className="w-[29%]">
                      {t("Execution info")}
                    </TableHead>
                    <TableHead>{t("Result")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tasks.length === 0 ? (
                    <TableRow>
                      <TableCell
                        colSpan={3}
                        className="text-muted-foreground h-32 text-center"
                      >
                        {t("No priority task records")}
                      </TableCell>
                    </TableRow>
                  ) : (
                    tasks.map((task) => (
                      <TableRow key={task.task_id} className="align-top">
                        <TableCell className="align-top whitespace-normal">
                          <div className="min-w-0 space-y-1.5">
                            <div className="flex flex-wrap gap-1">
                              <Badge variant="outline">
                                {t(TASK_TRIGGER_LABEL[task.trigger])}
                              </Badge>
                              <Badge
                                variant={
                                  task.status === "failed"
                                    ? "destructive"
                                    : "secondary"
                                }
                                className={TASK_STATUS_CLASS_NAME[task.status]}
                              >
                                {t(task.status)}
                              </Badge>
                            </div>
                            <Button
                              type="button"
                              variant="ghost"
                              size="xs"
                              className="h-auto max-w-full justify-start px-0 font-mono text-xs"
                              title={task.task_id}
                              aria-label={t("Copy task ID")}
                              onClick={() =>
                                void handleCopyTaskId(task.task_id)
                              }
                            >
                              <span className="truncate">{task.task_id}</span>
                              <Copy className="size-3.5" />
                            </Button>
                          </div>
                        </TableCell>
                        <TableCell className="align-top whitespace-normal">
                          <dl className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-1 text-xs">
                            <dt className="text-muted-foreground">
                              {t("Start time")}
                            </dt>
                            <dd className="break-words">
                              {formatTimestampToDate(
                                task.started_at ?? undefined,
                              )}
                            </dd>
                            <dt className="text-muted-foreground">
                              {t("Completion time")}
                            </dt>
                            <dd className="break-words">
                              {formatTimestampToDate(
                                task.completed_at ?? undefined,
                              )}
                            </dd>
                            <dt className="text-muted-foreground">
                              {t("Duration")}
                            </dt>
                            <dd>{formatTaskDuration(task.duration_ms, t)}</dd>
                          </dl>
                        </TableCell>
                        <TableCell className="align-top whitespace-normal">
                          <TaskResultSummary task={task} />
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          )}
        </div>

        <DialogFooter className="flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <AlertDialog>
            <AlertDialogTrigger
              render={
                <Button
                  type="button"
                  variant="destructive"
                  disabled={
                    loading || clearing || hasActiveTasks || total === 0
                  }
                />
              }
            >
              {clearing ? (
                <LoaderCircle className="animate-spin" />
              ) : (
                <Trash2 />
              )}
              {t(clearing ? "Clearing..." : "Clear list")}
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {t("Clear priority task records?")}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {t(
                    "This will permanently delete all completed priority task records. This action cannot be undone.",
                  )}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t("Cancel")}</AlertDialogCancel>
                <AlertDialogAction
                  variant="destructive"
                  onClick={() => void handleClearTasks()}
                >
                  {t("Permanently delete")}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>

          <div className="flex flex-wrap items-center justify-center gap-2 text-sm">
            <span className="text-muted-foreground">
              {t("{{count}} records", { count: total })}
            </span>
            <Select
              value={String(pageSize)}
              onValueChange={(value) => {
                setPageSize(Number(value));
                setPage(1);
              }}
            >
              <SelectTrigger className="h-8 w-[92px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TASK_PAGE_SIZE_OPTIONS.map((option) => (
                  <SelectItem key={option} value={String(option)}>
                    {t("{{count}} / page", { count: option })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              type="button"
              variant="outline"
              size="icon-sm"
              disabled={loading || page <= 1}
              aria-label={t("Previous page")}
              onClick={() => setPage((current) => Math.max(1, current - 1))}
            >
              <ChevronLeft />
            </Button>
            <span className="min-w-16 text-center">
              {t("{{page}} / {{pageCount}}", { page, pageCount })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="icon-sm"
              disabled={loading || page >= pageCount}
              aria-label={t("Next page")}
              onClick={() =>
                setPage((current) => Math.min(pageCount, current + 1))
              }
            >
              <ChevronRight />
            </Button>
          </div>

          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={loading || refreshing}
              onClick={() => void loadTasks("manual")}
            >
              <RefreshCw className={refreshing ? "animate-spin" : undefined} />
              {t(refreshing ? "Refreshing..." : "Refresh")}
            </Button>
            <Button type="button" onClick={() => props.onOpenChange(false)}>
              {t("Close")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface UpstreamPriorityScheduleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function UpstreamPriorityScheduleDialog(
  props: UpstreamPriorityScheduleDialogProps,
) {
  const { t } = useTranslation();
  const [enabled, setEnabled] = useState(false);
  const [intervalSeconds, setIntervalSeconds] = useState("300");
  const [maxTestLatencySeconds, setMaxTestLatencySeconds] = useState("5");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [taskListOpen, setTaskListOpen] = useState(false);
  const [lastTestTask, setLastTestTask] =
    useState<UpstreamPriorityTaskRecord | null>(null);
  const [errorMessage, setErrorMessage] = useState("");

  useEffect(() => {
    if (!props.open) return;

    let active = true;
    setLoading(true);
    setErrorMessage("");
    setLastTestTask(null);

    void getManagedUpstreamPrioritySchedule()
      .then((response) => {
        if (!active) return;
        if (!response.success || !response.data) {
          setErrorMessage(
            response.message || t("Failed to load priority schedule"),
          );
          return;
        }
        setEnabled(response.data.enabled);
        setIntervalSeconds(String(response.data.interval_seconds));
        setMaxTestLatencySeconds(
          String(response.data.max_test_latency_seconds),
        );
      })
      .catch((error) => {
        if (!active) return;
        setErrorMessage(
          getRequestErrorMessage(error, t("Failed to load priority schedule")),
        );
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [props.open, t]);

  function handleOpenChange(nextOpen: boolean) {
    if (saving || testing) return;
    if (!nextOpen) setTaskListOpen(false);
    props.onOpenChange(nextOpen);
  }

  async function handleTestRun() {
    if (loading || saving || testing) return;

    setTesting(true);
    setErrorMessage("");
    setLastTestTask(null);
    try {
      const response = await runManagedUpstreamPrioritySchedule();
      if (!response.success || !response.data) {
        const message = response.message || t("Failed to run priority test");
        if (response.data) setLastTestTask(response.data);
        setErrorMessage(message);
        toast.error(message);
        return;
      }

      const task = response.data;
      const summary = formatTaskResult(task.result, t);
      const taskIssues = getTaskIssues(task);
      setLastTestTask(task);

      if (task.status === "succeeded") {
        toast.success(t("Priority test completed: {{summary}}", { summary }));
        return;
      }

      const message =
        (taskIssues[0] && getTaskIssueReason(taskIssues[0], t)) ||
        t("Priority test completed with status {{status}}", {
          status: t(task.status),
        });
      toast.error(message);
    } catch (error: unknown) {
      const responseError = error as {
        message?: string;
        response?: {
          data?: {
            message?: string;
            data?: UpstreamPriorityTaskRecord;
          };
        };
      };
      const task = responseError.response?.data?.data;
      if (task?.task_id) setLastTestTask(task);
      const message = getRequestErrorMessage(
        error,
        t("Failed to run priority test"),
      );
      setErrorMessage(message);
      toast.error(message);
    } finally {
      setTesting(false);
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (loading || saving || testing) return;

    const parsedIntervalSeconds = Number(intervalSeconds);
    if (
      !Number.isInteger(parsedIntervalSeconds) ||
      parsedIntervalSeconds < 15 ||
      parsedIntervalSeconds > 86400
    ) {
      setErrorMessage(
        t("Execution interval must be an integer between 15 and 86400 seconds"),
      );
      return;
    }

    const parsedMaxTestLatencySeconds = Number(maxTestLatencySeconds);
    if (
      !Number.isInteger(parsedMaxTestLatencySeconds) ||
      parsedMaxTestLatencySeconds < 1 ||
      parsedMaxTestLatencySeconds > 300
    ) {
      setErrorMessage(
        t("Maximum test latency must be an integer between 1 and 300 seconds"),
      );
      return;
    }

    setSaving(true);
    setErrorMessage("");
    try {
      const response = await updateManagedUpstreamPrioritySchedule({
        enabled,
        interval_seconds: parsedIntervalSeconds,
        max_test_latency_seconds: parsedMaxTestLatencySeconds,
      });
      if (!response.success) {
        setErrorMessage(
          response.message || t("Failed to save priority schedule"),
        );
        return;
      }
      toast.success(t("Priority schedule saved"));
      props.onOpenChange(false);
    } catch (error) {
      setErrorMessage(
        getRequestErrorMessage(error, t("Failed to save priority schedule")),
      );
    } finally {
      setSaving(false);
    }
  }

  let lastTestTitle = "";
  if (lastTestTask?.status === "succeeded") {
    lastTestTitle = t("Priority test completed");
  } else if (lastTestTask && isActiveTask(lastTestTask.status)) {
    lastTestTitle = t("Priority test task is {{status}}", {
      status: t(lastTestTask.status),
    });
  } else if (lastTestTask) {
    lastTestTitle = t("Priority test completed with status {{status}}", {
      status: t(lastTestTask.status),
    });
  }

  return (
    <>
      <Dialog open={props.open} onOpenChange={handleOpenChange}>
        <DialogContent
          showCloseButton={!saving && !testing}
          className="max-h-[90vh] overflow-y-auto sm:max-w-2xl"
        >
          <DialogHeader>
            <DialogTitle>{t("Cost-effective priority scheduling")}</DialogTitle>
            <DialogDescription>
              {t(
                "Configure a recurring safety-checked process that keeps usable, lower-cost upstream channels ahead in routing priority.",
              )}
            </DialogDescription>
          </DialogHeader>
          <PriorityScheduleLogicExplanation />

          <form className="space-y-4" onSubmit={handleSubmit}>
            {errorMessage && (
              <Alert variant="destructive">
                <AlertDescription>{errorMessage}</AlertDescription>
              </Alert>
            )}

            {lastTestTask && (
              <Alert
                variant={
                  lastTestTask.status === "failed" ? "destructive" : "default"
                }
              >
                <AlertDescription className="space-y-1">
                  <div className="font-medium">{lastTestTitle}</div>
                  <TaskResultSummary
                    key={lastTestTask.task_id}
                    task={lastTestTask}
                  />
                </AlertDescription>
              </Alert>
            )}

            {loading ? (
              <div className="text-muted-foreground flex min-h-32 items-center justify-center gap-2">
                <LoaderCircle className="size-4 animate-spin" />
                {t("Loading...")}
              </div>
            ) : (
              <fieldset className="space-y-4" disabled={saving || testing}>
                <div className="flex items-center justify-between gap-4 rounded-lg border p-3">
                  <Label htmlFor="upstream-priority-schedule-enabled">
                    {t("Enable scheduled priority adjustment")}
                  </Label>
                  <Switch
                    id="upstream-priority-schedule-enabled"
                    checked={enabled}
                    onCheckedChange={setEnabled}
                  />
                </div>

                {enabled && (
                  <div className="space-y-4 rounded-lg border p-3">
                    <div className="space-y-1.5">
                      <Label htmlFor="upstream-priority-schedule-interval">
                        {t("Execution interval")}
                      </Label>
                      <div className="flex items-center gap-2">
                        <Input
                          id="upstream-priority-schedule-interval"
                          type="number"
                          min="15"
                          max="86400"
                          step="1"
                          inputMode="numeric"
                          value={intervalSeconds}
                          onChange={(event) =>
                            setIntervalSeconds(event.target.value)
                          }
                        />
                        <span className="text-muted-foreground shrink-0">
                          {t("seconds")}
                        </span>
                      </div>
                    </div>

                    <div className="space-y-1.5">
                      <Label htmlFor="upstream-priority-schedule-latency">
                        {t("Maximum test latency")}
                      </Label>
                      <div className="flex items-center gap-2">
                        <Input
                          id="upstream-priority-schedule-latency"
                          type="number"
                          min="1"
                          max="300"
                          step="1"
                          inputMode="numeric"
                          value={maxTestLatencySeconds}
                          onChange={(event) =>
                            setMaxTestLatencySeconds(event.target.value)
                          }
                        />
                        <span className="text-muted-foreground shrink-0">
                          {t("seconds")}
                        </span>
                      </div>
                    </div>
                  </div>
                )}
              </fieldset>
            )}

            <DialogFooter className="sm:justify-between">
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={loading || saving || testing}
                  onClick={() => void handleTestRun()}
                >
                  {testing && <LoaderCircle className="animate-spin" />}
                  {t(testing ? "Testing..." : "Test")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={loading || saving || testing}
                  onClick={() => setTaskListOpen(true)}
                >
                  <ListChecks />
                  {t("Task list")}
                </Button>
              </div>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={saving || testing}
                  onClick={() => handleOpenChange(false)}
                >
                  {t("Cancel")}
                </Button>
                <Button type="submit" disabled={loading || saving || testing}>
                  {saving && <LoaderCircle className="animate-spin" />}
                  {t("Save schedule")}
                </Button>
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <PriorityTaskListDialog
        open={taskListOpen}
        onOpenChange={setTaskListOpen}
      />
    </>
  );
}
