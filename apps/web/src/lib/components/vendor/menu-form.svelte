<script lang="ts">
  import {
    Button,
    Card,
    ChipInput,
    DateInput,
    FileDropzone,
    FormField,
    MoneyInput,
    TimeInput
  } from "$lib/components/ui";
  import {
    friendlyHealthTag,
    friendlyMenuType
  } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";
  import {
    HEALTH_TAG_OPTIONS,
    MENU_TYPE_OPTIONS
  } from "$lib/vendor/helpers";
  import type {
    MenuHealthTag,
    MenuType
  } from "../../../../../../contract/generated/ts-client";

  export interface MenuDraft {
    menuItemId: string;
    deliveryDate: string;
    name: string;
    description: string;
    menuType: MenuType;
    healthTags: MenuHealthTag[];
    imageUrl: string;
    currency: string;
    amountMinor: number;
    maxDailyQuantity: number;
    preorderOpenDaysAheadOverride: string;
    modifyCancelCutoffMinuteOfDayOverride: string;
  }

  interface Props {
    draft: MenuDraft;
    submitLabel: string;
    submitting: boolean;
    lockMenuItemId?: boolean;
    onSubmit: () => void;
  }

  let {
    draft = $bindable(),
    submitLabel,
    submitting,
    lockMenuItemId = false,
    onSubmit
  }: Props = $props();

  // Health tag mapping: chip labels are zh-TW, enum values are sent to API.
  const HEALTH_TAG_FRIENDLY: string[] = HEALTH_TAG_OPTIONS.map((tag) =>
    friendlyHealthTag(tag)
  );
  const FRIENDLY_TO_ENUM: Record<string, MenuHealthTag> = Object.fromEntries(
    HEALTH_TAG_OPTIONS.map((tag) => [friendlyHealthTag(tag), tag])
  );

  // Reactive mirror of healthTags in friendly labels for ChipInput.
  let healthChips = $state<string[]>(draft.healthTags.map((t) => friendlyHealthTag(t)));

  // Cutoff override as minute-of-day integer; "" → no override.
  let cutoffMinute = $state<number>(
    draft.modifyCancelCutoffMinuteOfDayOverride
      ? Number.parseInt(draft.modifyCancelCutoffMinuteOfDayOverride, 10) || 1080
      : 1080
  );
  let cutoffEnabled = $state(draft.modifyCancelCutoffMinuteOfDayOverride !== "");

  let preorderEnabled = $state(draft.preorderOpenDaysAheadOverride !== "");

  function syncHealthTags(next: string[]) {
    const mapped: MenuHealthTag[] = [];
    for (const label of next) {
      const enumValue = FRIENDLY_TO_ENUM[label] ?? (label as MenuHealthTag);
      if (HEALTH_TAG_OPTIONS.includes(enumValue) && !mapped.includes(enumValue)) {
        mapped.push(enumValue);
      }
    }
    draft.healthTags = mapped;
  }

  function onCutoffChange(next: number) {
    cutoffMinute = next;
    draft.modifyCancelCutoffMinuteOfDayOverride = cutoffEnabled ? String(next) : "";
  }

  function toggleCutoff(checked: boolean) {
    cutoffEnabled = checked;
    draft.modifyCancelCutoffMinuteOfDayOverride = checked ? String(cutoffMinute) : "";
  }

  function togglePreorder(checked: boolean) {
    preorderEnabled = checked;
    if (!checked) draft.preorderOpenDaysAheadOverride = "";
    else if (!draft.preorderOpenDaysAheadOverride)
      draft.preorderOpenDaysAheadOverride = "3";
  }

  // Menu image upload: use FileDropzone to upload MENU_IMAGE, then write objectRef to imageUrl.
  let imageUploaded = $state(Boolean(draft.imageUrl));
  async function uploadMenuImage(file: File): Promise<{ objectRef: string }> {
    const response = await apiClient.vendor.createVendorObjectStorageUploadPlan({
      artifactClass: "MENU_IMAGE",
      fileName: file.name,
      mimeType: file.type || "application/octet-stream",
      sizeBytes: file.size
    });
    const { uploadUrl, requiredHeaders, objectRef } = response.primary;
    const putResponse = await fetch(uploadUrl, {
      method: "PUT",
      headers: requiredHeaders,
      body: file
    });
    if (!putResponse.ok) {
      throw new Error(`圖片上傳失敗（HTTP ${putResponse.status}）`);
    }
    return { objectRef };
  }

  function onImageUploaded(objectRef: string) {
    draft.imageUrl = objectRef;
    imageUploaded = true;
  }

  function clearImage() {
    draft.imageUrl = "";
    imageUploaded = false;
  }
</script>

<Card title="菜單資料">
  <div class="grid gap-3 md:grid-cols-2">
    {#if lockMenuItemId}
      <FormField label="菜單編號">
        <code class="inline-flex items-center rounded bg-slate-50 px-2 py-1.5 font-mono text-xs text-slate-700 ring-1 ring-slate-200">
          {draft.menuItemId}
        </code>
      </FormField>
    {/if}

    <FormField label="配送日" required>
      <DateInput bind:value={draft.deliveryDate} />
    </FormField>

    <FormField label="名稱（1–80 字）" required>
      <input
        class="rounded border border-slate-300 bg-white px-2 py-1.5"
        maxlength="80"
        bind:value={draft.name}
      />
    </FormField>

    <FormField label="餐點類型" required>
      <select
        class="rounded border border-slate-300 bg-white px-2 py-1.5"
        bind:value={draft.menuType}
      >
        {#each MENU_TYPE_OPTIONS as type}
          <option value={type}>{friendlyMenuType(type)}</option>
        {/each}
      </select>
    </FormField>

    <FormField label="描述（1–280 字）" required>
      <textarea
        class="rounded border border-slate-300 bg-white px-2 py-1.5"
        rows="3"
        maxlength="280"
        bind:value={draft.description}
      ></textarea>
    </FormField>

    <FormField label="健康標籤">
      <ChipInput
        bind:values={healthChips}
        suggestions={HEALTH_TAG_FRIENDLY}
        placeholder="輸入或從下方建議加入"
        onchange={syncHealthTags}
      />
    </FormField>

    <FormField label="售價" required>
      <MoneyInput bind:value={draft.amountMinor} currency={draft.currency || "TWD"} min={0} />
    </FormField>

    <FormField label="每日份數上限（1–2000）" required>
      <input
        type="number"
        min="1"
        max="2000"
        class="rounded border border-slate-300 bg-white px-2 py-1.5"
        bind:value={draft.maxDailyQuantity}
      />
    </FormField>

    <FormField label="菜單圖片" hint="拖檔或點擊上傳">
      {#if imageUploaded}
        <div class="grid gap-2">
          {#if draft.imageUrl.startsWith("http")}
            <img
              src={draft.imageUrl}
              alt="菜單圖片預覽"
              class="h-28 w-28 rounded border border-slate-200 object-cover"
            />
          {/if}
          <code class="break-all rounded bg-slate-50 px-2 py-1 font-mono text-[11px] text-slate-700 ring-1 ring-slate-200">
            {draft.imageUrl}
          </code>
          <div>
            <Button size="sm" variant="ghost" onclick={clearImage}>重新上傳</Button>
          </div>
        </div>
      {:else}
        <FileDropzone
          plan={uploadMenuImage}
          accept="image/*"
          maxSizeBytes={10 * 1024 * 1024}
          label="拖曳菜單圖片至此"
          hint="JPG / PNG / WEBP，最大 10 MB"
          onuploaded={onImageUploaded}
        />
      {/if}
    </FormField>
  </div>

  <details class="rounded-lg border border-slate-200 bg-slate-50/50 px-3 py-2">
    <summary class="cursor-pointer text-sm font-semibold text-slate-700">
      進階訂購設定（可選）
    </summary>
    <div class="mt-3 grid gap-3 md:grid-cols-2">
      <FormField label="預購開放天數 override（1–7）" hint="未勾選則沿用全店政策">
        <div class="grid gap-2">
          <label class="inline-flex items-center gap-2 text-sm text-slate-700">
            <input
              type="checkbox"
              checked={preorderEnabled}
              onchange={(event) => togglePreorder((event.currentTarget as HTMLInputElement).checked)}
            />
            為此菜單覆蓋預購開放天數
          </label>
          {#if preorderEnabled}
            <input
              type="number"
              min="1"
              max="7"
              class="rounded border border-slate-300 bg-white px-2 py-1.5"
              bind:value={draft.preorderOpenDaysAheadOverride}
            />
          {/if}
        </div>
      </FormField>

      <FormField label="前日截單時間 override" hint="未勾選則沿用全店政策">
        <div class="grid gap-2">
          <label class="inline-flex items-center gap-2 text-sm text-slate-700">
            <input
              type="checkbox"
              checked={cutoffEnabled}
              onchange={(event) => toggleCutoff((event.currentTarget as HTMLInputElement).checked)}
            />
            為此菜單覆蓋截單時間
          </label>
          {#if cutoffEnabled}
            <TimeInput
              value={cutoffMinute}
              min={900}
              max={1200}
              step={900}
              onchange={onCutoffChange}
            />
          {/if}
        </div>
      </FormField>
    </div>
  </details>

  <div class="flex justify-end">
    <Button variant="primary" onclick={onSubmit} loading={submitting}>{submitLabel}</Button>
  </div>
</Card>
