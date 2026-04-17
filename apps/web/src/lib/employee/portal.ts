const PICKUP_REFRESH_INTERVAL_SECONDS = 30;

export interface CountdownView {
  secondsRemaining: number;
  expired: boolean;
  label: string;
}

export function isEmployeeOrderEditable(status: string): boolean {
  return status === "PENDING" || status === "MODIFIED";
}

export function isPickupEligible(status: string): boolean {
  return isEmployeeOrderEditable(status);
}

export function isResolvedDisputeStatus(status: string): boolean {
  return status.startsWith("RESOLVED_");
}

export function taipeiDateMinuteToEpochMs(dateIso: string, minuteOfDay: number): number {
  if (!Number.isInteger(minuteOfDay) || minuteOfDay < 0 || minuteOfDay > 1439) {
    throw new Error("minuteOfDay must be an integer between 0 and 1439");
  }

  const hour = Math.floor(minuteOfDay / 60);
  const minute = minuteOfDay % 60;
  const hourText = `${hour}`.padStart(2, "0");
  const minuteText = `${minute}`.padStart(2, "0");
  const epochMs = Date.parse(`${dateIso}T${hourText}:${minuteText}:00+08:00`);

  if (Number.isNaN(epochMs)) {
    throw new Error("dateIso must be a valid YYYY-MM-DD date string");
  }

  return epochMs;
}

export function countdownToEpoch(nowEpochMs: number, targetEpochMs: number): CountdownView {
  const secondsRemaining = Math.max(0, Math.ceil((targetEpochMs - nowEpochMs) / 1000));
  if (secondsRemaining === 0) {
    return {
      secondsRemaining,
      expired: true,
      label: "已截止"
    };
  }

  return {
    secondsRemaining,
    expired: false,
    label: `倒數 ${formatDuration(secondsRemaining)}`
  };
}

export function pickupQrSecondsRemaining(
  nowEpochMs: number,
  qr: {
    expiresAtEpochSecond: number;
  }
): number {
  const remaining = Math.ceil(qr.expiresAtEpochSecond - nowEpochMs / 1000);
  return Math.max(0, remaining);
}

export function pickupRefreshIntervalSeconds(): number {
  return PICKUP_REFRESH_INTERVAL_SECONDS;
}

function formatDuration(totalSeconds: number): string {
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (days > 0) {
    return `${days}天 ${hours.toString().padStart(2, "0")}:${minutes
      .toString()
      .padStart(2, "0")}:${seconds.toString().padStart(2, "0")}`;
  }

  return `${hours.toString().padStart(2, "0")}:${minutes
    .toString()
    .padStart(2, "0")}:${seconds.toString().padStart(2, "0")}`;
}
