// Drives the unmodified SDK client against a fresh instance: 21 steps, from the
// viewer to a rejected key. Data is created through the public API on the empty
// database the bootstrap left behind; identifiers come from the create results
// and are never written as literals.
import { randomBytes } from "node:crypto";
import { LinearClient, AuthenticationLinearError, InvalidInputLinearError } from "@linear/sdk";
import { createRunner, assertEqual, assertMatch, assertMultiset, StepFailure, need } from "../lib/check.mjs";

// The workspace the bootstrap sets up.
const WORKSPACE_NAME = "CI Workspace";
const slugOf = (name) => name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");

const ids = (issues) => issues.map((i) => i.identifier);

// A lookup inside a step that found nothing is a failure of this step. Values stored by
// an earlier step go through need(), which marks the step as a consequence instead.
function found(value, what) {
  if (value === undefined || value === null) throw new StepFailure(`${what} was not found`);
  return value;
}

// The text of a thrown value for step messages: class name and first line.
const describeError = (e) => `${e?.constructor?.name}: ${String(e?.message).split("\n")[0]}`;

// Runs fn, which must throw; returns the thrown error.
async function rejection(fn, what) {
  try {
    await fn();
  } catch (e) {
    return e;
  }
  throw new StepFailure(`${what}: no error was thrown`);
}

export async function run(ctx) {
  const runner = createRunner("sdk");
  const client = new LinearClient({ apiKey: ctx.apiKey, apiUrl: ctx.baseUrl + "/graphql" });
  const { alp, bet } = ctx.projects;

  // A token that occurs in one title of this run only.
  const token = `cmp${randomBytes(4).toString("hex")}`;
  const titles = { created: `Retry bank sync ${token}`, child: "Child task" };
  const dueDate = "2026-12-01";

  let me, alpTeam, betTeam, todo, inProgress, betDone, memberUser, bug, milestone;
  let createPayload, created, createdIdentifier, child, childIdentifier, movedIdentifier;

  await runner.step("viewer", async () => {
    me = await client.viewer;
    assertEqual(me.isMe, true, "isMe");
    assertEqual(me.owner, true, "owner");
    assertEqual(me.name, ctx.owner.name, "name");
    assertEqual(me.id, ctx.owner.id, "id");
  });

  await runner.step("organization", async () => {
    const org = await client.organization;
    assertEqual(org.name, WORKSPACE_NAME, "name");
    assertEqual(org.urlKey, slugOf(WORKSPACE_NAME), "urlKey");
    assertEqual(org.projectStatuses.length, 4, "project statuses");
  });

  await runner.step("teams", async () => {
    const teams = await client.teams();
    alpTeam = teams.nodes.find((t) => t.key === alp.key);
    betTeam = teams.nodes.find((t) => t.key === bet.key);
    found(alpTeam, `team ${alp.key}`);
    found(betTeam, `team ${bet.key}`);
    assertEqual(alpTeam.id, alp.id, `${alp.key} id`);
    assertEqual(betTeam.id, bet.id, `${bet.key} id`);
  });

  await runner.step("team.states", async () => {
    const states = await need(alpTeam, "team", "teams").states();
    assertEqual(states.nodes.length, 5, "states of the first team");
    todo = states.nodes.find((s) => s.name === "Todo");
    inProgress = states.nodes.find((s) => s.type === "started");
    betDone = (await need(betTeam, "team", "teams").states()).nodes.find((s) => s.type === "completed");
    found(todo, "state Todo");
    found(inProgress, "a started state");
    found(betDone, "a completed state of the second team");
  });

  await runner.step("users", async () => {
    const users = await client.users();
    memberUser = users.nodes.find((u) => u.email === ctx.member.email);
    found(memberUser, `user ${ctx.member.email}`);
    assertEqual(memberUser.id, ctx.member.id, "member id");
  });

  await runner.step("createIssueLabel, updateIssueLabel", async () => {
    const payload = await client.createIssueLabel({ name: "Bug report", color: "#EB5757" });
    assertEqual(payload.success, true, "create success");
    bug = await payload.issueLabel;
    assertEqual(bug.name, "Bug report", "created name");
    assertEqual(String(bug.color).toLowerCase(), "#eb5757", "created color");
    const updated = await (await client.updateIssueLabel(bug.id, { name: "Bug!" })).issueLabel;
    assertEqual(updated.name, "Bug!", "updated name");
    assertEqual(String(updated.color).toLowerCase(), "#eb5757", "color after update");
    bug = updated;
  });

  await runner.step("projects", async () => {
    const projects = await client.projects();
    milestone = projects.nodes.find((p) => p.name === ctx.milestone.name);
    found(milestone, `project ${ctx.milestone.name}`);
    assertEqual(milestone.id, ctx.milestone.id, "milestone id");
  });

  await runner.step("createIssue with labels, assignee, project, due date", async () => {
    const payload = await client.createIssue({
      teamId: alp.id,
      title: titles.created,
      description: "From the SDK",
      priority: 2,
      labelIds: [need(bug, "label", "createIssueLabel, updateIssueLabel").id],
      assigneeId: need(me, "viewer", "viewer").id,
      projectId: need(milestone, "milestone", "projects").id,
      dueDate,
    });
    assertEqual(payload.success, true, "success");
    createPayload = payload;
    created = await payload.issue;
    createdIdentifier = created.identifier;
    assertMatch(createdIdentifier, new RegExp(`^${alp.key}-\\d+$`), "identifier");
    assertMatch(created.branchName, /\S/, "branchName");
    assertEqual(created.labelIds.includes(bug.id), true, "label id returned");
    assertEqual(created.dueDate, dueDate, "dueDate");
  });

  await runner.step("sub-issue with parentId", async () => {
    const payload = await client.createIssue({
      teamId: alp.key,
      title: titles.child,
      parentId: need(createdIdentifier, "identifier", "createIssue with labels, assignee, project, due date"),
    });
    child = await payload.issue;
    childIdentifier = child.identifier;
    assertMatch(childIdentifier, new RegExp(`^${alp.key}-\\d+$`), "child identifier");
    const parent = await child.parent;
    const project = await child.project;
    assertEqual(parent?.id, created.id, "parent id");
    assertEqual(project?.id, milestone.id, "inherited project id");
  });

  await runner.step("lazy getters", async () => {
    const issue = await client.issue(need(created, "issue", "createIssue with labels, assignee, project, due date").id);
    const [state, team, assignee, project, creator] = await Promise.all([
      issue.state,
      issue.team,
      issue.assignee,
      issue.project,
      issue.creator,
    ]);
    assertEqual(state?.id, todo.id, "state");
    assertEqual(team?.id, alpTeam.id, "team");
    assertEqual(assignee?.id, me.id, "assignee");
    assertEqual(project?.id, milestone.id, "project");
    assertEqual(creator?.id, me.id, "creator");
    const viaPayload = await need(createPayload, "create payload", "createIssue with labels, assignee, project, due date").issue;
    assertEqual(viaPayload?.id, issue.id, "payload.issue");
    assertEqual((await viaPayload.team)?.id, alpTeam.id, "payload.issue team");
  });

  await runner.step("issue by lower-cased identifier", async () => {
    const issue = await client.issue(need(createdIdentifier, "identifier", "createIssue with labels, assignee, project, due date").toLowerCase());
    assertEqual(issue.id, created.id, "id");
  });

  await runner.step("updateIssue state, assignee, removed label", async () => {
    const payload = await client.updateIssue(need(created, "issue", "createIssue with labels, assignee, project, due date").id, {
      stateId: inProgress.id,
      assigneeId: memberUser.id,
      addedLabelIds: [],
      removedLabelIds: [bug.id],
    });
    const issue = await payload.issue;
    const [state, assignee] = await Promise.all([issue.state, issue.assignee]);
    assertEqual(state?.type, "started", "state type");
    assertEqual(assignee?.id, memberUser.id, "assignee");
    assertEqual(issue.labelIds.includes(bug.id), false, "label removed");
  });

  await runner.step("issues filters", async () => {
    need(child, "sub-issue", "sub-issue with parentId");
    await client.updateIssue(child.id, { labelIds: [bug.id], assigneeId: me.id });
    const childFresh = await client.issue(child.id);
    const childState = await childFresh.state;
    // What each issue of this run looks like now; the expected sets follow from it.
    const model = [
      { identifier: createdIdentifier, title: titles.created, priority: 2, assigneeId: memberUser.id, stateType: "started", labelIds: [] },
      {
        identifier: childIdentifier,
        title: titles.child,
        priority: childFresh.priority,
        assigneeId: me.id,
        stateType: childState.type,
        labelIds: [bug.id],
      },
    ];
    const expected = (pred) => ids(model.filter(pred));
    const got = async (filter) => ids((await client.issues({ filter })).nodes);

    assertMultiset(await got({ assignee: { isMe: { eq: true } } }), expected((i) => i.assigneeId === me.id), "isMe");
    assertMultiset(await got({ state: { type: { eq: "started" } } }), expected((i) => i.stateType === "started"), "state type");
    assertMultiset(await got({ labels: { name: { eq: bug.name } } }), expected((i) => i.labelIds.includes(bug.id)), "label name");
    assertMultiset(
      await got({ title: { containsIgnoreCase: token.toUpperCase() } }),
      expected((i) => i.title.toLowerCase().includes(token)),
      "title containsIgnoreCase",
    );
    assertMultiset(
      await got({ or: [{ title: { contains: titles.child } }, { priority: { eq: 2 } }] }),
      expected((i) => i.title.includes(titles.child) || i.priority === 2),
      "or",
    );
  });

  await runner.step("searchIssues", async () => {
    const result = await client.searchIssues(token);
    assertEqual(result.totalCount, 1, "totalCount");
    assertEqual(result.nodes[0]?.identifier, need(createdIdentifier, "identifier", "createIssue with labels, assignee, project, due date"), "hit");
    assertEqual(result.nodes[0]?.title, titles.created, "hit title");
  });

  await runner.step("comment create, update, delete", async () => {
    const issueId = need(createdIdentifier, "identifier", "createIssue with labels, assignee, project, due date");
    const comment = await (await client.createComment({ issueId, body: "Looks good" })).comment;
    assertEqual(comment.body, "Looks good", "created body");
    const updated = await (await client.updateComment(comment.id, { body: "Looks good, merged" })).comment;
    assertEqual(updated.body, "Looks good, merged", "updated body");
    assertEqual(Boolean(updated.editedAt), true, "editedAt set after the update");
    const issue = await client.issue(created.id);
    assertEqual((await issue.comments()).nodes.length, 1, "comments before delete");
    const deleted = await client.deleteComment(comment.id);
    assertEqual(deleted.success, true, "delete success");
    assertEqual((await issue.comments()).nodes.length, 0, "comments after delete");
  });

  await runner.step("move to the other team", async () => {
    need(createdIdentifier, "identifier", "createIssue with labels, assignee, project, due date");
    const payload = await client.updateIssue(created.id, { teamId: betTeam.id, stateId: betDone.id });
    const issue = await payload.issue;
    movedIdentifier = issue.identifier;
    assertMatch(movedIdentifier, new RegExp(`^${bet.key}-\\d+$`), "new identifier");
    assertEqual((await issue.team)?.key, bet.key, "team key");
    const old = await client.issue(createdIdentifier);
    assertEqual(old.id, created.id, "the old identifier resolves to the same issue");
  });

  await runner.step("move together with other fields is rejected", async () => {
    // A sub-issue follows its parent to the other team, so the target is whichever
    // team the sub-issue is not in now.
    const current = await (await client.issue(need(child, "sub-issue", "sub-issue with parentId").id)).team;
    const target = current?.id === alpTeam.id ? betTeam : alpTeam;
    const e = await rejection(() => client.updateIssue(child.id, { teamId: target.id, title: "x" }), "move with a title");
    if (!(e instanceof InvalidInputLinearError)) {
      throw new StepFailure(`expected InvalidInputLinearError, got ${describeError(e)}`);
    }
  });

  await runner.step("estimate in the create input is rejected", async () => {
    const e = await rejection(
      () => client.createIssue({ teamId: alp.id, title: "x", estimate: 3 }),
      "createIssue with estimate",
    );
    const texts = [e?.message, ...(e?.errors ?? []).map((x) => x?.message)].map(String);
    if (!texts.some((t) => /estimate/i.test(t))) {
      throw new StepFailure(`the error does not name the field estimate: ${describeError(e)}`);
    }
  });

  await runner.step("deleteIssue", async () => {
    const id = need(child, "sub-issue", "sub-issue with parentId").id;
    const deleted = await client.deleteIssue(id);
    assertEqual(deleted.success, true, "success");
    assertEqual(deleted.entityId, id, "entityId");
    for (const ref of [id, childIdentifier]) {
      const e = await rejection(() => client.issue(ref), "lookup of the deleted issue");
      if (!(e instanceof InvalidInputLinearError)) {
        throw new StepFailure(`expected InvalidInputLinearError for the lookup, got ${describeError(e)}`);
      }
    }
  });

  await runner.step("project create, status update, delete", async () => {
    const org = await client.organization;
    const done = org.projectStatuses.find((s) => s.type === "completed");
    found(done, "a completed project status");
    const created = await (await client.createProject({ teamIds: [alp.id], name: `Payments ${token}`, content: "# Scope" })).project;
    const pinned = await (await client.updateProject(created.id, { statusId: done.id })).project;
    const status = await pinned.status;
    assertEqual(status?.name, "Done", "pinned status");
    assertEqual(pinned.state, "completed", "pinned state");
    const back = await (await client.updateProject(created.id, { statusId: null })).project;
    assertEqual((await back.status)?.name, "Planned", "automatic status");
    const deleted = await client.deleteProject(created.id);
    assertEqual(deleted.success, true, "delete success");
  });

  await runner.step("wrong key", async () => {
    const bad = new LinearClient({ apiKey: "yal_api_wrong", apiUrl: ctx.baseUrl + "/graphql" });
    const e = await rejection(() => bad.viewer, "viewer with a wrong key");
    if (!(e instanceof AuthenticationLinearError)) {
      throw new StepFailure(`expected AuthenticationLinearError, got ${describeError(e)}`);
    }
  });

  return runner;
}
