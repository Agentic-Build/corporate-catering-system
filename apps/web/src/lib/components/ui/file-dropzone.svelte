<script lang="ts">
  /**
   * Drop-in file upload component.
   *
   * Parent provides a `plan` async function that receives the File and is
   * responsible for (1) creating the upload plan via API, (2) PUTting the
   * bytes, (3) returning the final { objectRef }.
   *
   * The component presents a dropzone, reads File name/type/size, runs
   * validation (mime / size), and tracks progress.
   *
   * Parent never touches mimeType / sizeBytes / uploadUrl.
   */
  interface Props {
    accept?: string; // MIME list for <input accept="">
    maxSizeBytes?: number;
    label?: string;
    hint?: string;
    disabled?: boolean;
    plan: (file: File) => Promise<{ objectRef: string }>;
    onuploaded?: (objectRef: string, file: File) => void;
  }

  let {
    accept = "",
    maxSizeBytes,
    label = "拖檔案到此或點擊選取",
    hint,
    disabled = false,
    plan,
    onuploaded
  }: Props = $props();

  let fileName = $state<string | null>(null);
  let status = $state<"idle" | "uploading" | "done" | "error">("idle");
  let errorMessage = $state<string | null>(null);
  let objectRef = $state<string | null>(null);
  let dragOver = $state(false);

  let inputEl: HTMLInputElement | null = $state(null);

  async function handleFiles(files: FileList | null) {
    const file = files?.[0];
    if (!file) return;
    fileName = file.name;
    errorMessage = null;
    objectRef = null;

    if (maxSizeBytes && file.size > maxSizeBytes) {
      status = "error";
      errorMessage = `檔案過大（${formatBytes(file.size)}），最大允許 ${formatBytes(maxSizeBytes)}`;
      return;
    }

    status = "uploading";
    try {
      const result = await plan(file);
      objectRef = result.objectRef;
      status = "done";
      onuploaded?.(result.objectRef, file);
    } catch (err) {
      status = "error";
      errorMessage = err instanceof Error ? err.message : "上傳失敗";
    }
  }

  function onDrop(event: DragEvent) {
    event.preventDefault();
    dragOver = false;
    if (disabled) return;
    handleFiles(event.dataTransfer?.files ?? null);
  }

  function onDragOver(event: DragEvent) {
    event.preventDefault();
    if (!disabled) dragOver = true;
  }

  function onDragLeave() {
    dragOver = false;
  }

  function onChange(event: Event) {
    const target = event.currentTarget as HTMLInputElement;
    handleFiles(target.files);
  }

  function openPicker() {
    if (disabled) return;
    inputEl?.click();
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  const classes = $derived(
    `grid cursor-pointer gap-2 rounded-xl border-2 border-dashed p-5 text-center transition ${
      disabled
        ? "border-slate-200 bg-slate-50/40 text-slate-400"
        : dragOver
          ? "border-cyan-500 bg-cyan-50"
          : status === "done"
            ? "border-emerald-300 bg-emerald-50"
            : status === "error"
              ? "border-rose-300 bg-rose-50"
              : "border-slate-300 bg-white hover:border-cyan-500"
    }`
  );
</script>

<div
  class={classes}
  role="button"
  tabindex={disabled ? -1 : 0}
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
  onclick={openPicker}
  onkeydown={(e) => (e.key === "Enter" || e.key === " ") && openPicker()}
>
  <input
    bind:this={inputEl}
    class="hidden"
    type="file"
    {accept}
    {disabled}
    onchange={onChange}
  />

  {#if status === "idle"}
    <p class="text-sm font-semibold text-slate-700">{label}</p>
    {#if hint}
      <p class="text-xs text-slate-500">{hint}</p>
    {/if}
  {:else if status === "uploading"}
    <p class="text-sm font-semibold text-slate-700">
      <span class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-cyan-600 border-t-transparent"></span>
      {" "}
      上傳中：{fileName}
    </p>
  {:else if status === "done"}
    <p class="text-sm font-semibold text-emerald-800">✓ 已上傳：{fileName}</p>
    <p class="text-[11px] font-mono text-emerald-700">{objectRef}</p>
  {:else if status === "error"}
    <p class="text-sm font-semibold text-rose-800">✗ 上傳失敗：{fileName}</p>
    {#if errorMessage}
      <p class="text-xs text-rose-700">{errorMessage}</p>
    {/if}
  {/if}
</div>
