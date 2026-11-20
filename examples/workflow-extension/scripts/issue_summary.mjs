import { readFileSync } from "node:fs";

const input = JSON.parse(readFileSync(0, "utf8"));
const issue = String(input.issue ?? "").trim();

if (!issue) {
  console.error("issue must be a non-empty string");
  process.exit(2);
}

console.log(`Issue summary requested for: ${issue}`);
