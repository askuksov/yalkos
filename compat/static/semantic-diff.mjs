// Compares the supported operations of two bundles by field paths, argument
// entries ("<fieldPath>(<argName>)"), variable names and variable types.
// Arguments are separate entries so that adding or removing one leaves the
// field subtree untouched. Order is ignored.
import { parse, print } from "graphql";
import { OPERATIONS } from "./extract-documents.mjs";

function analyze(doc) {
  const ast = parse(doc);
  const fragments = {};
  for (const d of ast.definitions) if (d.kind === "FragmentDefinition") fragments[d.name.value] = d;
  const paths = new Set();
  const vars = new Map();
  const walk = (set, prefix) => {
    if (!set) return;
    for (const s of set.selections) {
      if (s.kind === "Field") {
        const p = (prefix ? prefix + "." : "") + s.name.value;
        paths.add(p);
        for (const a of s.arguments || []) paths.add(`${p}(${a.name.value})`);
        walk(s.selectionSet, p);
      } else if (s.kind === "FragmentSpread") {
        walk(fragments[s.name.value]?.selectionSet, prefix);
      } else if (s.kind === "InlineFragment") {
        const cond = s.typeCondition?.name.value;
        walk(s.selectionSet, cond ? `${prefix}|${cond}` : prefix);
      }
    }
  };
  for (const d of ast.definitions) {
    if (d.kind !== "OperationDefinition") continue;
    for (const v of d.variableDefinitions || []) vars.set("$" + v.variable.name.value, print(v.type));
    walk(d.selectionSet, "");
  }
  return { paths, vars, canonical: print(ast) };
}

const minus = (a, b) => [...a].filter((x) => !b.has(x)).sort();

export function semanticDiff(allOld, allNew) {
  const changed = [];
  let orderOnly = 0;
  for (const op of OPERATIONS) {
    if (!allOld[op] || !allNew[op]) continue;
    const x = analyze(allOld[op].doc);
    const y = analyze(allNew[op].doc);
    const oldNames = new Set(x.vars.keys());
    const newNames = new Set(y.vars.keys());
    const retypedVars = [...x.vars.keys()]
      .filter((n) => y.vars.has(n) && y.vars.get(n) !== x.vars.get(n))
      .sort()
      .map((n) => ({ variable: n, from: x.vars.get(n), to: y.vars.get(n) }));
    const entry = {
      op,
      addedFields: minus(y.paths, x.paths),
      removedFields: minus(x.paths, y.paths),
      addedVars: minus(newNames, oldNames).map((n) => `${n}: ${y.vars.get(n)}`),
      removedVars: minus(oldNames, newNames).map((n) => `${n}: ${x.vars.get(n)}`),
      retypedVars,
    };
    const differs = entry.addedFields.length || entry.removedFields.length || entry.addedVars.length ||
      entry.removedVars.length || entry.retypedVars.length;
    if (differs) changed.push(entry);
    else if (x.canonical !== y.canonical) orderOnly++;
  }
  return { changed, orderOnly };
}
