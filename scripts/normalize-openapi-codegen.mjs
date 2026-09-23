import fs from "node:fs";

const [, , inputPath, outputPath] = process.argv;
if (!inputPath || !outputPath) {
  throw new Error("usage: normalize-openapi-codegen.mjs <input.json> <output.json>");
}

const document = JSON.parse(fs.readFileSync(inputPath, "utf8"));
document.openapi = "3.0.3";
delete document.jsonSchemaDialect;

function normalize(node) {
  if (!node || typeof node !== "object") return;

  if (Array.isArray(node.type) && node.type.includes("null")) {
    const nonNullTypes = node.type.filter((type) => type !== "null");
    if (nonNullTypes.length === 1) {
      node.type = nonNullTypes[0];
      node.nullable = true;
    }
  }

  if (Array.isArray(node.oneOf) && node.oneOf.length === 2) {
    const nullBranch = node.oneOf.find((branch) => branch && branch.type === "null");
    const valueBranch = node.oneOf.find((branch) => !(branch && branch.type === "null"));
    if (nullBranch && valueBranch) {
      for (const [key, value] of Object.entries(valueBranch)) node[key] = value;
      delete node.oneOf;
      node.nullable = true;
    }
  }

  for (const value of Object.values(node)) {
    if (value && typeof value === "object") normalize(value);
  }
}

normalize(document);
fs.writeFileSync(outputPath, JSON.stringify(document, null, 2) + "\n");
