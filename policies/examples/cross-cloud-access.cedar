// Allow the API workload to read pipeline data
permit(
  principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
  action == Action::"read-data",
  resource == Resource::"pipeline-x"
);

// Allow the API workload to write pipeline results
permit(
  principal == SpiffeWorkload::"spiffe://remote.example.com/workload/api",
  action == Action::"write-results",
  resource == Resource::"pipeline-x"
);

// Deny the batch workload from writing to pipeline data
forbid(
  principal == SpiffeWorkload::"spiffe://remote.example.com/workload/batch",
  action == Action::"write-data",
  resource == Resource::"pipeline-x"
);
