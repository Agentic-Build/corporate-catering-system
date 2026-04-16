<svelte:head>
  <title>Corporate Catering Portal</title>
</svelte:head>

<main>
  <h1>Corporate Catering Portal (Mock Auth)</h1>
  <p>This phase is restricted to CLAR-002 mock authentication only.</p>

  <ul>
    <li><a href="/auth/mock?role=employee&next=/employee">Sign in as employee</a></li>
    <li><a href="/auth/mock?role=vendor&next=/vendor">Sign in as vendor</a></li>
    <li><a href="/auth/mock?role=admin&next=/admin">Sign in as admin</a></li>
    <li><a href="/auth/mock?logout=1&next=/">Clear current session</a></li>
  </ul>
</main>
