export default (sigma) => {
  sigma.registerTool(
    "workflow_echo",
    {
      description: "Echo text for the workflow extension demo",
      inputSchema: {
        type: "object",
        properties: { text: { type: "string" } },
        required: ["text"],
        additionalProperties: false,
      },
    },
    async ({ text }) => `Extension received: ${text}`,
  );

  sigma.registerCommand(
    "review",
    { description: "Run a small review workflow" },
    async (args, ui) => {
      ui.notify(`Review requested: ${args || "current task"}`);
      if (await ui.confirm("Continue with the review workflow?")) {
        ui.notify("Review approved.");
      } else {
        ui.notify("Review cancelled.");
      }
    },
  );

  sigma.on("session_start", (event) => {
    sigma.notify(`Workflow extension loaded for session ${event.session_id}`);
  });

  sigma.on("turn_start", (event) => {
    sigma.notify(`Turn ${event.turn} started`);
  });

  sigma.on("tool_complete", (event) => {
    if (event.status === "failed") {
      sigma.notify(`${event.tool} failed: ${event.error}`);
    }
  });
};
