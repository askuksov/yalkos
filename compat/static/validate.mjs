// Validates documents with graphql-js against the Linear-compatible schema
// files; used to classify errors the Go check reports.
import fs from "node:fs";
import path from "node:path";
import { parse, buildASTSchema, extendSchema, validate } from "graphql";

function loadSchema(schemaDir) {
  const files = fs.readdirSync(schemaDir).filter((f) => f.endsWith(".graphqls")).sort();
  if (files.length === 0) throw new Error(`no .graphqls files in ${schemaDir}`);
  const doc = parse(files.map((f) => fs.readFileSync(path.join(schemaDir, f), "utf8")).join("\n"));
  // Extensions are applied after the base types, whatever the file order.
  const base = { kind: "Document", definitions: doc.definitions.filter((d) => !d.kind.endsWith("Extension")) };
  const ext = { kind: "Document", definitions: doc.definitions.filter((d) => d.kind.endsWith("Extension")) };
  let schema = buildASTSchema(base, { assumeValidSDL: true });
  if (ext.definitions.length) schema = extendSchema(schema, ext, { assumeValidSDL: true });
  return schema;
}

// Returns [{ file, messages }] for every *.graphql file; no messages = valid.
export function validateDocuments({ schemaDir, docsDir }) {
  const schema = loadSchema(schemaDir);
  return fs
    .readdirSync(docsDir)
    .filter((f) => f.endsWith(".graphql"))
    .sort()
    .map((file) => {
      try {
        const errors = validate(schema, parse(fs.readFileSync(path.join(docsDir, file), "utf8")));
        return { file, messages: errors.map((e) => e.message) };
      } catch (err) {
        return { file, messages: [`Syntax error: ${err.message}`] };
      }
    });
}
