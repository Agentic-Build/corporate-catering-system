<script lang="ts">
    import { onMount } from 'svelte';

    let currentYear = new Date().getFullYear();
    let currentMonth = new Date().getMonth() + 1;

    let selectedYear = currentYear;
    let selectedMonth = currentMonth;

    let isExporting = false;
    let exportMessage = '';

    const years = Array.from({ length: 5 }, (_, i) => currentYear - i);
    const months = Array.from({ length: 12 }, (_, i) => i + 1);

    async function handleExport() {
        isExporting = true;
        exportMessage = '';
        try {
            // In a real app, this would call the actual API endpoint:
            // e.g. GET /api/v1/admin/quota/reports?year=${selectedYear}&month=${selectedMonth}
            // and trigger a file download.
            
            // Simulating API call
            await new Promise(resolve => setTimeout(resolve, 1000));
            exportMessage = `Successfully exported Quota Report for ${selectedYear}-${selectedMonth.toString().padStart(2, '0')}`;
        } catch (error) {
            exportMessage = 'Failed to export report.';
            console.error(error);
        } finally {
            isExporting = false;
        }
    }
</script>

<div class="container mx-auto p-8">
    <header class="mb-8">
        <h1 class="text-3xl font-bold text-gray-900">Quota Usage Reports</h1>
        <p class="text-gray-600 mt-2">Export monthly quota usage reports for all employees.</p>
    </header>

    <div class="bg-white rounded-lg shadow p-6 max-w-xl">
        <div class="flex flex-col space-y-6">
            <div class="grid grid-cols-2 gap-4">
                <div>
                    <label for="year" class="block text-sm font-medium text-gray-700">Year</label>
                    <select id="year" bind:value={selectedYear} class="mt-1 block w-full pl-3 pr-10 py-2 text-base border-gray-300 focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm rounded-md">
                        {#each years as year}
                            <option value={year}>{year}</option>
                        {/each}
                    </select>
                </div>
                <div>
                    <label for="month" class="block text-sm font-medium text-gray-700">Month</label>
                    <select id="month" bind:value={selectedMonth} class="mt-1 block w-full pl-3 pr-10 py-2 text-base border-gray-300 focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm rounded-md">
                        {#each months as month}
                            <option value={month}>{month}</option>
                        {/each}
                    </select>
                </div>
            </div>

            <div>
                <button 
                    on:click={handleExport} 
                    disabled={isExporting}
                    class="w-full flex justify-center py-2 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50"
                >
                    {isExporting ? 'Exporting...' : 'Export CSV Report'}
                </button>
            </div>

            {#if exportMessage}
                <div class="mt-4 p-4 rounded-md {exportMessage.includes('Successfully') ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'}">
                    {exportMessage}
                </div>
            {/if}
        </div>
    </div>
</div>
