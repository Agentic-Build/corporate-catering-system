<script lang="ts">
  import { goto } from "$app/navigation";

  import { Card, Button, FormField, ChipInput, toasts } from "$lib/components/ui";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    VENDOR_CATEGORY_OPTIONS,
    type VendorCategory
  } from "$lib/admin/api";
  import { friendlyVendorCategory } from "$lib/platform/labels";

  interface InitialValues {
    vendorCategory: VendorCategory;
    templateId: string;
    displayName: string;
    required: boolean;
    maxValidityDays: number;
    reminderDaysBeforeExpiryCsv: string;
    suspensionGraceDays: number;
  }

  interface Props {
    mode: "create" | "edit";
    initial: InitialValues;
    apiBearerToken: string | null;
    /**
     * When true, vendorCategory + templateId are locked (editing existing key).
     */
    lockKey?: boolean;
  }

  let { mode, initial, apiBearerToken, lockKey = false }: Props = $props();

  let draft = $state(
    ((init: InitialValues) => ({
      ...init,
      reminderDays: init.reminderDaysBeforeExpiryCsv
        .split(",")
        .map((entry) => entry.trim())
        .filter((entry) => entry.length > 0)
    }))(initial)
  );
  let submitting = $state(false);
  let formError = $state<string | null>(null);

  function slugify(input: string): string {
    return input
      .toLowerCase()
      .trim()
      .replace(/[^\p{Letter}\p{Number}]+/gu, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 64);
  }

  function autoGenerateTemplateId() {
    if (lockKey) return;
    const slug = slugify(draft.displayName);
    if (slug.length > 0) {
      draft.templateId = slug;
    }
  }

  function validateReminderDay(raw: string): string | null {
    const parsed = Number(raw);
    if (!Number.isInteger(parsed) || parsed < 0) {
      return "提醒天數需為 ≥ 0 的整數";
    }
    return null;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = null;

    const templateId = draft.templateId.trim();
    const displayName = draft.displayName.trim();
    if (templateId.length === 0 || displayName.length === 0) {
      formError = "Template ID 與顯示名稱不可為空。";
      return;
    }
    if (!Number.isInteger(draft.maxValidityDays) || draft.maxValidityDays < 1) {
      formError = "有效天數必須是 ≥ 1 的整數。";
      return;
    }
    if (!Number.isInteger(draft.suspensionGraceDays) || draft.suspensionGraceDays < 0) {
      formError = "停權寬限必須是 ≥ 0 的整數。";
      return;
    }
    if (draft.reminderDays.length === 0) {
      formError = "至少要有一個提醒天數。";
      return;
    }

    let reminders: number[];
    try {
      reminders = draft.reminderDays.map((raw) => {
        const parsed = Number(raw);
        if (!Number.isInteger(parsed) || parsed < 0) {
          throw new Error(`提醒天數 \`${raw}\` 無效`);
        }
        return parsed;
      });
    } catch (error) {
      formError = error instanceof Error ? error.message : "提醒天數格式無效。";
      return;
    }

    submitting = true;
    try {
      configureAdminApi(apiBearerToken);
      await apiClient.admin.upsertComplianceDocumentTemplate(
        draft.vendorCategory,
        templateId,
        {
          displayName,
          required: draft.required,
          maxValidityDays: draft.maxValidityDays,
          reminderDaysBeforeExpiry: reminders,
          suspensionGraceDays: draft.suspensionGraceDays
        }
      );
      toasts.success(`模板 ${templateId} 已儲存。`);
      await goto("/admin/compliance/templates");
    } catch (error) {
      const message = describeApiError(error);
      formError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }
</script>

<form class="grid gap-4" onsubmit={submit}>
  <Card title="身份" description="模板的識別資料；新建後 Template ID 與商家分類無法變更。">
    <div class="grid gap-3 md:grid-cols-2">
      <FormField label="商家分類" required>
        <select
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm disabled:bg-slate-100"
          disabled={lockKey}
          bind:value={draft.vendorCategory}
        >
          {#each VENDOR_CATEGORY_OPTIONS as option}
            <option value={option}>{friendlyVendorCategory(option)}</option>
          {/each}
        </select>
      </FormField>
      <FormField label="顯示名稱" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.displayName}
          placeholder="例：商業登記文件"
        />
      </FormField>
      <FormField label="Template ID" required>
        <div class="flex items-stretch gap-2">
          <input
            class="flex-1 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-mono disabled:bg-slate-100"
            disabled={lockKey}
            bind:value={draft.templateId}
            placeholder="例：business-license"
          />
          {#if !lockKey}
            <Button
              variant="ghost"
              type="button"
              onclick={autoGenerateTemplateId}
              title="根據顯示名稱自動產生"
            >
              自動生成
            </Button>
          {/if}
        </div>
      </FormField>
      <FormField label="是否必填">
        <label class="flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm">
          <input type="checkbox" bind:checked={draft.required} />
          這是必填文件
        </label>
      </FormField>
    </div>
  </Card>

  <Card title="有效期" description="文件每次上傳後最長可用的天數，以及逾期後的寬限。">
    <div class="grid gap-3 md:grid-cols-2">
      <FormField label="有效天數" hint="例：365 表一年；過期後需補件。" required>
        <input
          type="number"
          min="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.maxValidityDays}
        />
      </FormField>
      <FormField label="停權寬限（天）" hint="逾期後幾天未補件會自動停權。" required>
        <input
          type="number"
          min="0"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.suspensionGraceDays}
        />
      </FormField>
    </div>
  </Card>

  <Card title="提醒" description="到期前提前提醒商家補件。">
    <FormField label="提前提醒天數" hint="按 Enter 加入；建議 30 / 14 / 7 / 3 / 1。" required>
      <ChipInput
        bind:values={draft.reminderDays}
        placeholder="輸入天數，按 Enter"
        suggestions={["30", "14", "7", "3", "1"]}
        validate={validateReminderDay}
      />
    </FormField>
  </Card>

  {#if formError}
    <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
      {formError}
    </p>
  {/if}

  <div class="flex gap-2">
    <Button type="submit" variant="primary" loading={submitting}>
      {mode === "create" ? "建立模板" : "儲存變更"}
    </Button>
    <Button variant="ghost" href="/admin/compliance/templates">取消</Button>
  </div>
</form>
