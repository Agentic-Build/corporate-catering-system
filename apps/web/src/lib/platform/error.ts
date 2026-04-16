import { zhTW } from "$lib/i18n/zh-tw";

export interface LocalizedErrorCopy {
  title: string;
  description: string;
  statusText: string;
}

export function getLocalizedErrorCopy(status: number): LocalizedErrorCopy {
  const statusText =
    zhTW.error.statusText[status as keyof typeof zhTW.error.statusText] ??
    zhTW.error.genericDescription;

  return {
    title: zhTW.error.title,
    description: zhTW.error.genericDescription,
    statusText
  };
}
