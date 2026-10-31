/* eslint-disable */
import * as types from './graphql';
import type { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';

/**
 * Map of all GraphQL operations in the project.
 *
 * This map has several performance disadvantages:
 * 1. It is not tree-shakeable, so it will include all operations in the project.
 * 2. It is not minifiable, so the string of a GraphQL query will be multiple times inside the bundle.
 * 3. It does not support dead code elimination, so it will add unused operations.
 *
 * Therefore it is highly recommended to use the babel or swc plugin for production.
 * Learn more about it here: https://the-guild.dev/graphql/codegen/plugins/presets/preset-client#reducing-bundle-size
 */
type Documents = {
    "\n  query SetupStatus {\n    setupStatus { needed }\n  }\n": typeof types.SetupStatusDocument,
    "\n  query Viewer {\n    viewer { id name email role }\n  }\n": typeof types.ViewerDocument,
    "\n  mutation Setup($input: SetupInput!) {\n    setup(input: $input) { id }\n  }\n": typeof types.SetupDocument,
    "\n  mutation Login($email: String!, $password: String!) {\n    login(email: $email, password: $password) { id }\n  }\n": typeof types.LoginDocument,
    "\n  mutation Logout {\n    logout\n  }\n": typeof types.LogoutDocument,
    "\n  mutation AcceptInvite($input: AcceptInviteInput!) {\n    acceptInvite(input: $input) { id }\n  }\n": typeof types.AcceptInviteDocument,
    "\n  query Members {\n    members { id name email role disabled }\n  }\n": typeof types.MembersDocument,
    "\n  query Invites {\n    invites { id role expiresAt }\n  }\n": typeof types.InvitesDocument,
    "\n  mutation CreateInvite($role: Role!, $days: Int) {\n    createInvite(role: $role, expiresInDays: $days) { url invite { id } }\n  }\n": typeof types.CreateInviteDocument,
    "\n  mutation RevokeInvite($id: ID!) {\n    revokeInvite(id: $id)\n  }\n": typeof types.RevokeInviteDocument,
    "\n  mutation UpdateMemberRole($userId: ID!, $role: Role!) {\n    updateMemberRole(userId: $userId, role: $role) { id role }\n  }\n": typeof types.UpdateMemberRoleDocument,
    "\n  mutation SetMemberDisabled($userId: ID!, $disabled: Boolean!) {\n    setMemberDisabled(userId: $userId, disabled: $disabled) { id disabled }\n  }\n": typeof types.SetMemberDisabledDocument,
    "\n  query ApiKeys {\n    apiKeys { id name displayPrefix createdAt lastUsedAt }\n  }\n": typeof types.ApiKeysDocument,
    "\n  mutation CreateApiKey($name: String!) {\n    createApiKey(name: $name) { key apiKey { id } }\n  }\n": typeof types.CreateApiKeyDocument,
    "\n  mutation RevokeApiKey($id: ID!) {\n    revokeApiKey(id: $id)\n  }\n": typeof types.RevokeApiKeyDocument,
    "\n  query Projects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 250) {\n      nodes { id key name icon color archivedAt isMember }\n    }\n  }\n": typeof types.ProjectsDocument,
    "\n  query Project($id: String!) {\n    project(id: $id) {\n      id key name description icon color archivedAt isMember\n      states { id name type color position isDefault }\n      members { id name }\n    }\n  }\n": typeof types.ProjectDocument,
    "\n  query PaletteIssue($id: String!) {\n    issue(id: $id) { id identifier title project { id key } }\n  }\n": typeof types.PaletteIssueDocument,
    "\n  query PaletteSearch($filter: IssueFilter, $first: Int) {\n    issues(filter: $filter, first: $first, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title project { id key } state { name type color } }\n    }\n  }\n": typeof types.PaletteSearchDocument,
    "\n  query PaletteChildCount($id: String!) {\n    issue(id: $id) { id children(first: 1) { totalCount } milestone { id } }\n  }\n": typeof types.PaletteChildCountDocument,
    "\n  query PaletteMilestones {\n    projects(first: 100) { nodes { id key isMember milestones(includeDone: false) { id name } } }\n  }\n": typeof types.PaletteMilestonesDocument,
    "\n  query IssuePage($id: String!) {\n    issue(id: $id) {\n      ...IssueRowFields\n      number description createdAt updatedAt\n      creator { id name }\n      project { id key name archivedAt states { id name type color position isDefault } }\n      children(first: 1) { totalCount }\n    }\n  }\n": typeof types.IssuePageDocument,
    "\n  mutation MoveIssue($id: ID!, $projectId: ID!) {\n    moveIssue(id: $id, projectId: $projectId) { id identifier }\n  }\n": typeof types.MoveIssueDocument,
    "\n  query IssueTimeline($id: String!, $before: String) {\n    issue(id: $id) {\n      id\n      timeline(last: 50, before: $before) {\n        nodes {\n          __typename\n          ... on Comment { id body createdAt editedAt author { id name } }\n          ... on HistoryEntry { id action actor { id name } changes { field from to } createdAt }\n        }\n        pageInfo { hasPreviousPage startCursor }\n      }\n    }\n  }\n": typeof types.IssueTimelineDocument,
    "\n  query IssueAttachments($id: String!) {\n    issue(id: $id) {\n      id\n      attachments { id name size contentType url inline createdAt uploader { id name } }\n    }\n  }\n": typeof types.IssueAttachmentsDocument,
    "\n  mutation CreateComment($issueId: ID!, $body: String!, $attachmentIds: [ID!]) {\n    createComment(issueId: $issueId, body: $body, attachmentIds: $attachmentIds) { id body }\n  }\n": typeof types.CreateCommentDocument,
    "\n  mutation UpdateComment($id: ID!, $body: String!) {\n    updateComment(id: $id, body: $body) { id body editedAt }\n  }\n": typeof types.UpdateCommentDocument,
    "\n  mutation DeleteComment($id: ID!) {\n    deleteComment(id: $id)\n  }\n": typeof types.DeleteCommentDocument,
    "\n  mutation DeleteAttachment($id: ID!) {\n    deleteAttachment(id: $id)\n  }\n": typeof types.DeleteAttachmentDocument,
    "\n  query IssueRef($id: String!) {\n    issue(id: $id) { id identifier }\n  }\n": typeof types.IssueRefDocument,
    "\n  mutation CreateIssue($input: CreateIssueInput!) {\n    createIssue(input: $input) { id identifier project { id } }\n  }\n": typeof types.CreateIssueDocument,
    "\n  fragment IssueRowFields on Issue {\n    id identifier title priority dueDate sortOrder\n    state { id name type color }\n    assignee { id name }\n    parent { id identifier }\n    subIssueProgress { completed total }\n    project { id key archivedAt }\n    type { id name color }\n    labels { id name color }\n    milestone { id name }\n    commentCount\n  }\n": typeof types.IssueRowFieldsFragmentDoc,
    "\n  query IssueGroup($filter: IssueFilter, $orderBy: IssueOrder, $first: Int, $after: String) {\n    issues(filter: $filter, orderBy: $orderBy, first: $first, after: $after) {\n      totalCount\n      nodes { ...IssueRowFields }\n      pageInfo { hasNextPage endCursor }\n    }\n  }\n": typeof types.IssueGroupDocument,
    "\n  query IssueChildren($id: String!, $orderBy: IssueOrder) {\n    issue(id: $id) {\n      id\n      # 200 rows keep the query under the API cost limit (each row carries labels).\n      children(first: 200, orderBy: $orderBy) { nodes { ...IssueRowFields } }\n    }\n  }\n": typeof types.IssueChildrenDocument,
    "\n  mutation UpdateIssue($id: ID!, $input: UpdateIssueInput!) {\n    updateIssue(id: $id, input: $input) { ...IssueRowFields }\n  }\n": typeof types.UpdateIssueDocument,
    "\n  mutation DeleteIssue($id: ID!) {\n    deleteIssue(id: $id)\n  }\n": typeof types.DeleteIssueDocument,
    "\n  mutation UnarchiveProject($id: ID!) {\n    unarchiveProject(id: $id) { id archivedAt }\n  }\n": typeof types.UnarchiveProjectDocument,
    "\n  fragment LabelFields on Label {\n    id name color description isGroup exclusive position parent { id }\n  }\n": typeof types.LabelFieldsFragmentDoc,
    "\n  query Labels {\n    labels { ...LabelFields children { ...LabelFields } }\n    labelSettings { typeGroup { id } defaultType { id } }\n  }\n": typeof types.LabelsDocument,
    "\n  mutation CreateLabel($input: CreateLabelInput!) {\n    createLabel(input: $input) { ...LabelFields }\n  }\n": typeof types.CreateLabelDocument,
    "\n  mutation UpdateLabel($id: ID!, $input: UpdateLabelInput!) {\n    updateLabel(id: $id, input: $input) { ...LabelFields }\n  }\n": typeof types.UpdateLabelDocument,
    "\n  mutation DeleteLabel($id: ID!) {\n    deleteLabel(id: $id)\n  }\n": typeof types.DeleteLabelDocument,
    "\n  mutation SetDefaultType($labelId: ID) {\n    setDefaultType(labelId: $labelId) { defaultType { id } }\n  }\n": typeof types.SetDefaultTypeDocument,
    "\n  query LabelIssueCount($id: ID!) {\n    issues(filter: { label: { in: [$id] } }, first: 1) { totalCount }\n  }\n": typeof types.LabelIssueCountDocument,
    "\n  query IssueLink($id: String!) {\n    issue(id: $id) { id identifier title state { id name type color } }\n  }\n": typeof types.IssueLinkDocument,
    "\n  query Limits {\n    limits { maxUploadMb }\n  }\n": typeof types.LimitsDocument,
    "\n  fragment MilestoneFields on Milestone {\n    id name targetDate status statusAutomatic overdue\n    progress { completed total }\n    lead { id name }\n    project { id key }\n  }\n": typeof types.MilestoneFieldsFragmentDoc,
    "\n  query Milestones($projectId: String!) {\n    project(id: $projectId) { id milestones { ...MilestoneFields } }\n  }\n": typeof types.MilestonesDocument,
    "\n  query MilestonePage($id: ID!) {\n    milestone(id: $id) { ...MilestoneFields description createdBy { id } }\n  }\n": typeof types.MilestonePageDocument,
    "\n  mutation CreateMilestone($projectId: ID!, $input: CreateMilestoneInput!) {\n    createMilestone(projectId: $projectId, input: $input) { id }\n  }\n": typeof types.CreateMilestoneDocument,
    "\n  mutation UpdateMilestone($id: ID!, $input: UpdateMilestoneInput!) {\n    updateMilestone(id: $id, input: $input) { ...MilestoneFields description }\n  }\n": typeof types.UpdateMilestoneDocument,
    "\n  mutation ReorderMilestones($projectId: ID!, $milestoneIds: [ID!]!) {\n    reorderMilestones(projectId: $projectId, milestoneIds: $milestoneIds) { id }\n  }\n": typeof types.ReorderMilestonesDocument,
    "\n  mutation DeleteMilestone($id: ID!) {\n    deleteMilestone(id: $id)\n  }\n": typeof types.DeleteMilestoneDocument,
    "\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n": typeof types.PickIssuesDocument,
    "\n  query BrowseProjects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 100) {\n      nodes { id key name description color archivedAt isMember members { id } }\n    }\n  }\n": typeof types.BrowseProjectsDocument,
    "\n  mutation CreateProject($input: CreateProjectInput!) {\n    createProject(input: $input) { id key }\n  }\n": typeof types.CreateProjectDocument,
    "\n  mutation UpdateProject($id: ID!, $input: UpdateProjectInput!) {\n    updateProject(id: $id, input: $input) { id key name description icon color }\n  }\n": typeof types.UpdateProjectDocument,
    "\n  mutation ArchiveProject($id: ID!) {\n    archiveProject(id: $id) { id archivedAt }\n  }\n": typeof types.ArchiveProjectDocument,
    "\n  mutation DeleteProject($id: ID!, $confirmKey: String!) {\n    deleteProject(id: $id, confirmKey: $confirmKey)\n  }\n": typeof types.DeleteProjectDocument,
    "\n  mutation AddProjectMember($projectId: ID!, $userId: ID!) {\n    addProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n": typeof types.AddProjectMemberDocument,
    "\n  mutation RemoveProjectMember($projectId: ID!, $userId: ID!) {\n    removeProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n": typeof types.RemoveProjectMemberDocument,
    "\n  mutation CreateState($projectId: ID!, $input: CreateStateInput!) {\n    createState(projectId: $projectId, input: $input) { id }\n  }\n": typeof types.CreateStateDocument,
    "\n  mutation UpdateState($id: ID!, $input: UpdateStateInput!) {\n    updateState(id: $id, input: $input) { id name color isDefault }\n  }\n": typeof types.UpdateStateDocument,
    "\n  mutation ReorderStates($projectId: ID!, $stateIds: [ID!]!) {\n    reorderStates(projectId: $projectId, stateIds: $stateIds) { id position }\n  }\n": typeof types.ReorderStatesDocument,
    "\n  mutation DeleteState($id: ID!, $moveIssuesTo: ID) {\n    deleteState(id: $id, moveIssuesTo: $moveIssuesTo)\n  }\n": typeof types.DeleteStateDocument,
    "\n  query StateIssueCount($filter: IssueFilter) {\n    issues(filter: $filter, first: 1) { totalCount }\n  }\n": typeof types.StateIssueCountDocument,
};
const documents: Documents = {
    "\n  query SetupStatus {\n    setupStatus { needed }\n  }\n": types.SetupStatusDocument,
    "\n  query Viewer {\n    viewer { id name email role }\n  }\n": types.ViewerDocument,
    "\n  mutation Setup($input: SetupInput!) {\n    setup(input: $input) { id }\n  }\n": types.SetupDocument,
    "\n  mutation Login($email: String!, $password: String!) {\n    login(email: $email, password: $password) { id }\n  }\n": types.LoginDocument,
    "\n  mutation Logout {\n    logout\n  }\n": types.LogoutDocument,
    "\n  mutation AcceptInvite($input: AcceptInviteInput!) {\n    acceptInvite(input: $input) { id }\n  }\n": types.AcceptInviteDocument,
    "\n  query Members {\n    members { id name email role disabled }\n  }\n": types.MembersDocument,
    "\n  query Invites {\n    invites { id role expiresAt }\n  }\n": types.InvitesDocument,
    "\n  mutation CreateInvite($role: Role!, $days: Int) {\n    createInvite(role: $role, expiresInDays: $days) { url invite { id } }\n  }\n": types.CreateInviteDocument,
    "\n  mutation RevokeInvite($id: ID!) {\n    revokeInvite(id: $id)\n  }\n": types.RevokeInviteDocument,
    "\n  mutation UpdateMemberRole($userId: ID!, $role: Role!) {\n    updateMemberRole(userId: $userId, role: $role) { id role }\n  }\n": types.UpdateMemberRoleDocument,
    "\n  mutation SetMemberDisabled($userId: ID!, $disabled: Boolean!) {\n    setMemberDisabled(userId: $userId, disabled: $disabled) { id disabled }\n  }\n": types.SetMemberDisabledDocument,
    "\n  query ApiKeys {\n    apiKeys { id name displayPrefix createdAt lastUsedAt }\n  }\n": types.ApiKeysDocument,
    "\n  mutation CreateApiKey($name: String!) {\n    createApiKey(name: $name) { key apiKey { id } }\n  }\n": types.CreateApiKeyDocument,
    "\n  mutation RevokeApiKey($id: ID!) {\n    revokeApiKey(id: $id)\n  }\n": types.RevokeApiKeyDocument,
    "\n  query Projects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 250) {\n      nodes { id key name icon color archivedAt isMember }\n    }\n  }\n": types.ProjectsDocument,
    "\n  query Project($id: String!) {\n    project(id: $id) {\n      id key name description icon color archivedAt isMember\n      states { id name type color position isDefault }\n      members { id name }\n    }\n  }\n": types.ProjectDocument,
    "\n  query PaletteIssue($id: String!) {\n    issue(id: $id) { id identifier title project { id key } }\n  }\n": types.PaletteIssueDocument,
    "\n  query PaletteSearch($filter: IssueFilter, $first: Int) {\n    issues(filter: $filter, first: $first, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title project { id key } state { name type color } }\n    }\n  }\n": types.PaletteSearchDocument,
    "\n  query PaletteChildCount($id: String!) {\n    issue(id: $id) { id children(first: 1) { totalCount } milestone { id } }\n  }\n": types.PaletteChildCountDocument,
    "\n  query PaletteMilestones {\n    projects(first: 100) { nodes { id key isMember milestones(includeDone: false) { id name } } }\n  }\n": types.PaletteMilestonesDocument,
    "\n  query IssuePage($id: String!) {\n    issue(id: $id) {\n      ...IssueRowFields\n      number description createdAt updatedAt\n      creator { id name }\n      project { id key name archivedAt states { id name type color position isDefault } }\n      children(first: 1) { totalCount }\n    }\n  }\n": types.IssuePageDocument,
    "\n  mutation MoveIssue($id: ID!, $projectId: ID!) {\n    moveIssue(id: $id, projectId: $projectId) { id identifier }\n  }\n": types.MoveIssueDocument,
    "\n  query IssueTimeline($id: String!, $before: String) {\n    issue(id: $id) {\n      id\n      timeline(last: 50, before: $before) {\n        nodes {\n          __typename\n          ... on Comment { id body createdAt editedAt author { id name } }\n          ... on HistoryEntry { id action actor { id name } changes { field from to } createdAt }\n        }\n        pageInfo { hasPreviousPage startCursor }\n      }\n    }\n  }\n": types.IssueTimelineDocument,
    "\n  query IssueAttachments($id: String!) {\n    issue(id: $id) {\n      id\n      attachments { id name size contentType url inline createdAt uploader { id name } }\n    }\n  }\n": types.IssueAttachmentsDocument,
    "\n  mutation CreateComment($issueId: ID!, $body: String!, $attachmentIds: [ID!]) {\n    createComment(issueId: $issueId, body: $body, attachmentIds: $attachmentIds) { id body }\n  }\n": types.CreateCommentDocument,
    "\n  mutation UpdateComment($id: ID!, $body: String!) {\n    updateComment(id: $id, body: $body) { id body editedAt }\n  }\n": types.UpdateCommentDocument,
    "\n  mutation DeleteComment($id: ID!) {\n    deleteComment(id: $id)\n  }\n": types.DeleteCommentDocument,
    "\n  mutation DeleteAttachment($id: ID!) {\n    deleteAttachment(id: $id)\n  }\n": types.DeleteAttachmentDocument,
    "\n  query IssueRef($id: String!) {\n    issue(id: $id) { id identifier }\n  }\n": types.IssueRefDocument,
    "\n  mutation CreateIssue($input: CreateIssueInput!) {\n    createIssue(input: $input) { id identifier project { id } }\n  }\n": types.CreateIssueDocument,
    "\n  fragment IssueRowFields on Issue {\n    id identifier title priority dueDate sortOrder\n    state { id name type color }\n    assignee { id name }\n    parent { id identifier }\n    subIssueProgress { completed total }\n    project { id key archivedAt }\n    type { id name color }\n    labels { id name color }\n    milestone { id name }\n    commentCount\n  }\n": types.IssueRowFieldsFragmentDoc,
    "\n  query IssueGroup($filter: IssueFilter, $orderBy: IssueOrder, $first: Int, $after: String) {\n    issues(filter: $filter, orderBy: $orderBy, first: $first, after: $after) {\n      totalCount\n      nodes { ...IssueRowFields }\n      pageInfo { hasNextPage endCursor }\n    }\n  }\n": types.IssueGroupDocument,
    "\n  query IssueChildren($id: String!, $orderBy: IssueOrder) {\n    issue(id: $id) {\n      id\n      # 200 rows keep the query under the API cost limit (each row carries labels).\n      children(first: 200, orderBy: $orderBy) { nodes { ...IssueRowFields } }\n    }\n  }\n": types.IssueChildrenDocument,
    "\n  mutation UpdateIssue($id: ID!, $input: UpdateIssueInput!) {\n    updateIssue(id: $id, input: $input) { ...IssueRowFields }\n  }\n": types.UpdateIssueDocument,
    "\n  mutation DeleteIssue($id: ID!) {\n    deleteIssue(id: $id)\n  }\n": types.DeleteIssueDocument,
    "\n  mutation UnarchiveProject($id: ID!) {\n    unarchiveProject(id: $id) { id archivedAt }\n  }\n": types.UnarchiveProjectDocument,
    "\n  fragment LabelFields on Label {\n    id name color description isGroup exclusive position parent { id }\n  }\n": types.LabelFieldsFragmentDoc,
    "\n  query Labels {\n    labels { ...LabelFields children { ...LabelFields } }\n    labelSettings { typeGroup { id } defaultType { id } }\n  }\n": types.LabelsDocument,
    "\n  mutation CreateLabel($input: CreateLabelInput!) {\n    createLabel(input: $input) { ...LabelFields }\n  }\n": types.CreateLabelDocument,
    "\n  mutation UpdateLabel($id: ID!, $input: UpdateLabelInput!) {\n    updateLabel(id: $id, input: $input) { ...LabelFields }\n  }\n": types.UpdateLabelDocument,
    "\n  mutation DeleteLabel($id: ID!) {\n    deleteLabel(id: $id)\n  }\n": types.DeleteLabelDocument,
    "\n  mutation SetDefaultType($labelId: ID) {\n    setDefaultType(labelId: $labelId) { defaultType { id } }\n  }\n": types.SetDefaultTypeDocument,
    "\n  query LabelIssueCount($id: ID!) {\n    issues(filter: { label: { in: [$id] } }, first: 1) { totalCount }\n  }\n": types.LabelIssueCountDocument,
    "\n  query IssueLink($id: String!) {\n    issue(id: $id) { id identifier title state { id name type color } }\n  }\n": types.IssueLinkDocument,
    "\n  query Limits {\n    limits { maxUploadMb }\n  }\n": types.LimitsDocument,
    "\n  fragment MilestoneFields on Milestone {\n    id name targetDate status statusAutomatic overdue\n    progress { completed total }\n    lead { id name }\n    project { id key }\n  }\n": types.MilestoneFieldsFragmentDoc,
    "\n  query Milestones($projectId: String!) {\n    project(id: $projectId) { id milestones { ...MilestoneFields } }\n  }\n": types.MilestonesDocument,
    "\n  query MilestonePage($id: ID!) {\n    milestone(id: $id) { ...MilestoneFields description createdBy { id } }\n  }\n": types.MilestonePageDocument,
    "\n  mutation CreateMilestone($projectId: ID!, $input: CreateMilestoneInput!) {\n    createMilestone(projectId: $projectId, input: $input) { id }\n  }\n": types.CreateMilestoneDocument,
    "\n  mutation UpdateMilestone($id: ID!, $input: UpdateMilestoneInput!) {\n    updateMilestone(id: $id, input: $input) { ...MilestoneFields description }\n  }\n": types.UpdateMilestoneDocument,
    "\n  mutation ReorderMilestones($projectId: ID!, $milestoneIds: [ID!]!) {\n    reorderMilestones(projectId: $projectId, milestoneIds: $milestoneIds) { id }\n  }\n": types.ReorderMilestonesDocument,
    "\n  mutation DeleteMilestone($id: ID!) {\n    deleteMilestone(id: $id)\n  }\n": types.DeleteMilestoneDocument,
    "\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n": types.PickIssuesDocument,
    "\n  query BrowseProjects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 100) {\n      nodes { id key name description color archivedAt isMember members { id } }\n    }\n  }\n": types.BrowseProjectsDocument,
    "\n  mutation CreateProject($input: CreateProjectInput!) {\n    createProject(input: $input) { id key }\n  }\n": types.CreateProjectDocument,
    "\n  mutation UpdateProject($id: ID!, $input: UpdateProjectInput!) {\n    updateProject(id: $id, input: $input) { id key name description icon color }\n  }\n": types.UpdateProjectDocument,
    "\n  mutation ArchiveProject($id: ID!) {\n    archiveProject(id: $id) { id archivedAt }\n  }\n": types.ArchiveProjectDocument,
    "\n  mutation DeleteProject($id: ID!, $confirmKey: String!) {\n    deleteProject(id: $id, confirmKey: $confirmKey)\n  }\n": types.DeleteProjectDocument,
    "\n  mutation AddProjectMember($projectId: ID!, $userId: ID!) {\n    addProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n": types.AddProjectMemberDocument,
    "\n  mutation RemoveProjectMember($projectId: ID!, $userId: ID!) {\n    removeProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n": types.RemoveProjectMemberDocument,
    "\n  mutation CreateState($projectId: ID!, $input: CreateStateInput!) {\n    createState(projectId: $projectId, input: $input) { id }\n  }\n": types.CreateStateDocument,
    "\n  mutation UpdateState($id: ID!, $input: UpdateStateInput!) {\n    updateState(id: $id, input: $input) { id name color isDefault }\n  }\n": types.UpdateStateDocument,
    "\n  mutation ReorderStates($projectId: ID!, $stateIds: [ID!]!) {\n    reorderStates(projectId: $projectId, stateIds: $stateIds) { id position }\n  }\n": types.ReorderStatesDocument,
    "\n  mutation DeleteState($id: ID!, $moveIssuesTo: ID) {\n    deleteState(id: $id, moveIssuesTo: $moveIssuesTo)\n  }\n": types.DeleteStateDocument,
    "\n  query StateIssueCount($filter: IssueFilter) {\n    issues(filter: $filter, first: 1) { totalCount }\n  }\n": types.StateIssueCountDocument,
};

/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 *
 *
 * @example
 * ```ts
 * const query = graphql(`query GetUser($id: ID!) { user(id: $id) { name } }`);
 * ```
 *
 * The query argument is unknown!
 * Please regenerate the types.
 */
export function graphql(source: string): unknown;

/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query SetupStatus {\n    setupStatus { needed }\n  }\n"): (typeof documents)["\n  query SetupStatus {\n    setupStatus { needed }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Viewer {\n    viewer { id name email role }\n  }\n"): (typeof documents)["\n  query Viewer {\n    viewer { id name email role }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation Setup($input: SetupInput!) {\n    setup(input: $input) { id }\n  }\n"): (typeof documents)["\n  mutation Setup($input: SetupInput!) {\n    setup(input: $input) { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation Login($email: String!, $password: String!) {\n    login(email: $email, password: $password) { id }\n  }\n"): (typeof documents)["\n  mutation Login($email: String!, $password: String!) {\n    login(email: $email, password: $password) { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation Logout {\n    logout\n  }\n"): (typeof documents)["\n  mutation Logout {\n    logout\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation AcceptInvite($input: AcceptInviteInput!) {\n    acceptInvite(input: $input) { id }\n  }\n"): (typeof documents)["\n  mutation AcceptInvite($input: AcceptInviteInput!) {\n    acceptInvite(input: $input) { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Members {\n    members { id name email role disabled }\n  }\n"): (typeof documents)["\n  query Members {\n    members { id name email role disabled }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Invites {\n    invites { id role expiresAt }\n  }\n"): (typeof documents)["\n  query Invites {\n    invites { id role expiresAt }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateInvite($role: Role!, $days: Int) {\n    createInvite(role: $role, expiresInDays: $days) { url invite { id } }\n  }\n"): (typeof documents)["\n  mutation CreateInvite($role: Role!, $days: Int) {\n    createInvite(role: $role, expiresInDays: $days) { url invite { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation RevokeInvite($id: ID!) {\n    revokeInvite(id: $id)\n  }\n"): (typeof documents)["\n  mutation RevokeInvite($id: ID!) {\n    revokeInvite(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateMemberRole($userId: ID!, $role: Role!) {\n    updateMemberRole(userId: $userId, role: $role) { id role }\n  }\n"): (typeof documents)["\n  mutation UpdateMemberRole($userId: ID!, $role: Role!) {\n    updateMemberRole(userId: $userId, role: $role) { id role }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation SetMemberDisabled($userId: ID!, $disabled: Boolean!) {\n    setMemberDisabled(userId: $userId, disabled: $disabled) { id disabled }\n  }\n"): (typeof documents)["\n  mutation SetMemberDisabled($userId: ID!, $disabled: Boolean!) {\n    setMemberDisabled(userId: $userId, disabled: $disabled) { id disabled }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query ApiKeys {\n    apiKeys { id name displayPrefix createdAt lastUsedAt }\n  }\n"): (typeof documents)["\n  query ApiKeys {\n    apiKeys { id name displayPrefix createdAt lastUsedAt }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateApiKey($name: String!) {\n    createApiKey(name: $name) { key apiKey { id } }\n  }\n"): (typeof documents)["\n  mutation CreateApiKey($name: String!) {\n    createApiKey(name: $name) { key apiKey { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation RevokeApiKey($id: ID!) {\n    revokeApiKey(id: $id)\n  }\n"): (typeof documents)["\n  mutation RevokeApiKey($id: ID!) {\n    revokeApiKey(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Projects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 250) {\n      nodes { id key name icon color archivedAt isMember }\n    }\n  }\n"): (typeof documents)["\n  query Projects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 250) {\n      nodes { id key name icon color archivedAt isMember }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Project($id: String!) {\n    project(id: $id) {\n      id key name description icon color archivedAt isMember\n      states { id name type color position isDefault }\n      members { id name }\n    }\n  }\n"): (typeof documents)["\n  query Project($id: String!) {\n    project(id: $id) {\n      id key name description icon color archivedAt isMember\n      states { id name type color position isDefault }\n      members { id name }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query PaletteIssue($id: String!) {\n    issue(id: $id) { id identifier title project { id key } }\n  }\n"): (typeof documents)["\n  query PaletteIssue($id: String!) {\n    issue(id: $id) { id identifier title project { id key } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query PaletteSearch($filter: IssueFilter, $first: Int) {\n    issues(filter: $filter, first: $first, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title project { id key } state { name type color } }\n    }\n  }\n"): (typeof documents)["\n  query PaletteSearch($filter: IssueFilter, $first: Int) {\n    issues(filter: $filter, first: $first, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title project { id key } state { name type color } }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query PaletteChildCount($id: String!) {\n    issue(id: $id) { id children(first: 1) { totalCount } milestone { id } }\n  }\n"): (typeof documents)["\n  query PaletteChildCount($id: String!) {\n    issue(id: $id) { id children(first: 1) { totalCount } milestone { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query PaletteMilestones {\n    projects(first: 100) { nodes { id key isMember milestones(includeDone: false) { id name } } }\n  }\n"): (typeof documents)["\n  query PaletteMilestones {\n    projects(first: 100) { nodes { id key isMember milestones(includeDone: false) { id name } } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssuePage($id: String!) {\n    issue(id: $id) {\n      ...IssueRowFields\n      number description createdAt updatedAt\n      creator { id name }\n      project { id key name archivedAt states { id name type color position isDefault } }\n      children(first: 1) { totalCount }\n    }\n  }\n"): (typeof documents)["\n  query IssuePage($id: String!) {\n    issue(id: $id) {\n      ...IssueRowFields\n      number description createdAt updatedAt\n      creator { id name }\n      project { id key name archivedAt states { id name type color position isDefault } }\n      children(first: 1) { totalCount }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation MoveIssue($id: ID!, $projectId: ID!) {\n    moveIssue(id: $id, projectId: $projectId) { id identifier }\n  }\n"): (typeof documents)["\n  mutation MoveIssue($id: ID!, $projectId: ID!) {\n    moveIssue(id: $id, projectId: $projectId) { id identifier }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssueTimeline($id: String!, $before: String) {\n    issue(id: $id) {\n      id\n      timeline(last: 50, before: $before) {\n        nodes {\n          __typename\n          ... on Comment { id body createdAt editedAt author { id name } }\n          ... on HistoryEntry { id action actor { id name } changes { field from to } createdAt }\n        }\n        pageInfo { hasPreviousPage startCursor }\n      }\n    }\n  }\n"): (typeof documents)["\n  query IssueTimeline($id: String!, $before: String) {\n    issue(id: $id) {\n      id\n      timeline(last: 50, before: $before) {\n        nodes {\n          __typename\n          ... on Comment { id body createdAt editedAt author { id name } }\n          ... on HistoryEntry { id action actor { id name } changes { field from to } createdAt }\n        }\n        pageInfo { hasPreviousPage startCursor }\n      }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssueAttachments($id: String!) {\n    issue(id: $id) {\n      id\n      attachments { id name size contentType url inline createdAt uploader { id name } }\n    }\n  }\n"): (typeof documents)["\n  query IssueAttachments($id: String!) {\n    issue(id: $id) {\n      id\n      attachments { id name size contentType url inline createdAt uploader { id name } }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateComment($issueId: ID!, $body: String!, $attachmentIds: [ID!]) {\n    createComment(issueId: $issueId, body: $body, attachmentIds: $attachmentIds) { id body }\n  }\n"): (typeof documents)["\n  mutation CreateComment($issueId: ID!, $body: String!, $attachmentIds: [ID!]) {\n    createComment(issueId: $issueId, body: $body, attachmentIds: $attachmentIds) { id body }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateComment($id: ID!, $body: String!) {\n    updateComment(id: $id, body: $body) { id body editedAt }\n  }\n"): (typeof documents)["\n  mutation UpdateComment($id: ID!, $body: String!) {\n    updateComment(id: $id, body: $body) { id body editedAt }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteComment($id: ID!) {\n    deleteComment(id: $id)\n  }\n"): (typeof documents)["\n  mutation DeleteComment($id: ID!) {\n    deleteComment(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteAttachment($id: ID!) {\n    deleteAttachment(id: $id)\n  }\n"): (typeof documents)["\n  mutation DeleteAttachment($id: ID!) {\n    deleteAttachment(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssueRef($id: String!) {\n    issue(id: $id) { id identifier }\n  }\n"): (typeof documents)["\n  query IssueRef($id: String!) {\n    issue(id: $id) { id identifier }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateIssue($input: CreateIssueInput!) {\n    createIssue(input: $input) { id identifier project { id } }\n  }\n"): (typeof documents)["\n  mutation CreateIssue($input: CreateIssueInput!) {\n    createIssue(input: $input) { id identifier project { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  fragment IssueRowFields on Issue {\n    id identifier title priority dueDate sortOrder\n    state { id name type color }\n    assignee { id name }\n    parent { id identifier }\n    subIssueProgress { completed total }\n    project { id key archivedAt }\n    type { id name color }\n    labels { id name color }\n    milestone { id name }\n    commentCount\n  }\n"): (typeof documents)["\n  fragment IssueRowFields on Issue {\n    id identifier title priority dueDate sortOrder\n    state { id name type color }\n    assignee { id name }\n    parent { id identifier }\n    subIssueProgress { completed total }\n    project { id key archivedAt }\n    type { id name color }\n    labels { id name color }\n    milestone { id name }\n    commentCount\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssueGroup($filter: IssueFilter, $orderBy: IssueOrder, $first: Int, $after: String) {\n    issues(filter: $filter, orderBy: $orderBy, first: $first, after: $after) {\n      totalCount\n      nodes { ...IssueRowFields }\n      pageInfo { hasNextPage endCursor }\n    }\n  }\n"): (typeof documents)["\n  query IssueGroup($filter: IssueFilter, $orderBy: IssueOrder, $first: Int, $after: String) {\n    issues(filter: $filter, orderBy: $orderBy, first: $first, after: $after) {\n      totalCount\n      nodes { ...IssueRowFields }\n      pageInfo { hasNextPage endCursor }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssueChildren($id: String!, $orderBy: IssueOrder) {\n    issue(id: $id) {\n      id\n      # 200 rows keep the query under the API cost limit (each row carries labels).\n      children(first: 200, orderBy: $orderBy) { nodes { ...IssueRowFields } }\n    }\n  }\n"): (typeof documents)["\n  query IssueChildren($id: String!, $orderBy: IssueOrder) {\n    issue(id: $id) {\n      id\n      # 200 rows keep the query under the API cost limit (each row carries labels).\n      children(first: 200, orderBy: $orderBy) { nodes { ...IssueRowFields } }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateIssue($id: ID!, $input: UpdateIssueInput!) {\n    updateIssue(id: $id, input: $input) { ...IssueRowFields }\n  }\n"): (typeof documents)["\n  mutation UpdateIssue($id: ID!, $input: UpdateIssueInput!) {\n    updateIssue(id: $id, input: $input) { ...IssueRowFields }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteIssue($id: ID!) {\n    deleteIssue(id: $id)\n  }\n"): (typeof documents)["\n  mutation DeleteIssue($id: ID!) {\n    deleteIssue(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UnarchiveProject($id: ID!) {\n    unarchiveProject(id: $id) { id archivedAt }\n  }\n"): (typeof documents)["\n  mutation UnarchiveProject($id: ID!) {\n    unarchiveProject(id: $id) { id archivedAt }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  fragment LabelFields on Label {\n    id name color description isGroup exclusive position parent { id }\n  }\n"): (typeof documents)["\n  fragment LabelFields on Label {\n    id name color description isGroup exclusive position parent { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Labels {\n    labels { ...LabelFields children { ...LabelFields } }\n    labelSettings { typeGroup { id } defaultType { id } }\n  }\n"): (typeof documents)["\n  query Labels {\n    labels { ...LabelFields children { ...LabelFields } }\n    labelSettings { typeGroup { id } defaultType { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateLabel($input: CreateLabelInput!) {\n    createLabel(input: $input) { ...LabelFields }\n  }\n"): (typeof documents)["\n  mutation CreateLabel($input: CreateLabelInput!) {\n    createLabel(input: $input) { ...LabelFields }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateLabel($id: ID!, $input: UpdateLabelInput!) {\n    updateLabel(id: $id, input: $input) { ...LabelFields }\n  }\n"): (typeof documents)["\n  mutation UpdateLabel($id: ID!, $input: UpdateLabelInput!) {\n    updateLabel(id: $id, input: $input) { ...LabelFields }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteLabel($id: ID!) {\n    deleteLabel(id: $id)\n  }\n"): (typeof documents)["\n  mutation DeleteLabel($id: ID!) {\n    deleteLabel(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation SetDefaultType($labelId: ID) {\n    setDefaultType(labelId: $labelId) { defaultType { id } }\n  }\n"): (typeof documents)["\n  mutation SetDefaultType($labelId: ID) {\n    setDefaultType(labelId: $labelId) { defaultType { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query LabelIssueCount($id: ID!) {\n    issues(filter: { label: { in: [$id] } }, first: 1) { totalCount }\n  }\n"): (typeof documents)["\n  query LabelIssueCount($id: ID!) {\n    issues(filter: { label: { in: [$id] } }, first: 1) { totalCount }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query IssueLink($id: String!) {\n    issue(id: $id) { id identifier title state { id name type color } }\n  }\n"): (typeof documents)["\n  query IssueLink($id: String!) {\n    issue(id: $id) { id identifier title state { id name type color } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Limits {\n    limits { maxUploadMb }\n  }\n"): (typeof documents)["\n  query Limits {\n    limits { maxUploadMb }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  fragment MilestoneFields on Milestone {\n    id name targetDate status statusAutomatic overdue\n    progress { completed total }\n    lead { id name }\n    project { id key }\n  }\n"): (typeof documents)["\n  fragment MilestoneFields on Milestone {\n    id name targetDate status statusAutomatic overdue\n    progress { completed total }\n    lead { id name }\n    project { id key }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Milestones($projectId: String!) {\n    project(id: $projectId) { id milestones { ...MilestoneFields } }\n  }\n"): (typeof documents)["\n  query Milestones($projectId: String!) {\n    project(id: $projectId) { id milestones { ...MilestoneFields } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query MilestonePage($id: ID!) {\n    milestone(id: $id) { ...MilestoneFields description createdBy { id } }\n  }\n"): (typeof documents)["\n  query MilestonePage($id: ID!) {\n    milestone(id: $id) { ...MilestoneFields description createdBy { id } }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateMilestone($projectId: ID!, $input: CreateMilestoneInput!) {\n    createMilestone(projectId: $projectId, input: $input) { id }\n  }\n"): (typeof documents)["\n  mutation CreateMilestone($projectId: ID!, $input: CreateMilestoneInput!) {\n    createMilestone(projectId: $projectId, input: $input) { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateMilestone($id: ID!, $input: UpdateMilestoneInput!) {\n    updateMilestone(id: $id, input: $input) { ...MilestoneFields description }\n  }\n"): (typeof documents)["\n  mutation UpdateMilestone($id: ID!, $input: UpdateMilestoneInput!) {\n    updateMilestone(id: $id, input: $input) { ...MilestoneFields description }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation ReorderMilestones($projectId: ID!, $milestoneIds: [ID!]!) {\n    reorderMilestones(projectId: $projectId, milestoneIds: $milestoneIds) { id }\n  }\n"): (typeof documents)["\n  mutation ReorderMilestones($projectId: ID!, $milestoneIds: [ID!]!) {\n    reorderMilestones(projectId: $projectId, milestoneIds: $milestoneIds) { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteMilestone($id: ID!) {\n    deleteMilestone(id: $id)\n  }\n"): (typeof documents)["\n  mutation DeleteMilestone($id: ID!) {\n    deleteMilestone(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n"): (typeof documents)["\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query BrowseProjects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 100) {\n      nodes { id key name description color archivedAt isMember members { id } }\n    }\n  }\n"): (typeof documents)["\n  query BrowseProjects($includeArchived: Boolean) {\n    projects(includeArchived: $includeArchived, first: 100) {\n      nodes { id key name description color archivedAt isMember members { id } }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateProject($input: CreateProjectInput!) {\n    createProject(input: $input) { id key }\n  }\n"): (typeof documents)["\n  mutation CreateProject($input: CreateProjectInput!) {\n    createProject(input: $input) { id key }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateProject($id: ID!, $input: UpdateProjectInput!) {\n    updateProject(id: $id, input: $input) { id key name description icon color }\n  }\n"): (typeof documents)["\n  mutation UpdateProject($id: ID!, $input: UpdateProjectInput!) {\n    updateProject(id: $id, input: $input) { id key name description icon color }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation ArchiveProject($id: ID!) {\n    archiveProject(id: $id) { id archivedAt }\n  }\n"): (typeof documents)["\n  mutation ArchiveProject($id: ID!) {\n    archiveProject(id: $id) { id archivedAt }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteProject($id: ID!, $confirmKey: String!) {\n    deleteProject(id: $id, confirmKey: $confirmKey)\n  }\n"): (typeof documents)["\n  mutation DeleteProject($id: ID!, $confirmKey: String!) {\n    deleteProject(id: $id, confirmKey: $confirmKey)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation AddProjectMember($projectId: ID!, $userId: ID!) {\n    addProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n"): (typeof documents)["\n  mutation AddProjectMember($projectId: ID!, $userId: ID!) {\n    addProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation RemoveProjectMember($projectId: ID!, $userId: ID!) {\n    removeProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n"): (typeof documents)["\n  mutation RemoveProjectMember($projectId: ID!, $userId: ID!) {\n    removeProjectMember(projectId: $projectId, userId: $userId) { id isMember }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateState($projectId: ID!, $input: CreateStateInput!) {\n    createState(projectId: $projectId, input: $input) { id }\n  }\n"): (typeof documents)["\n  mutation CreateState($projectId: ID!, $input: CreateStateInput!) {\n    createState(projectId: $projectId, input: $input) { id }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateState($id: ID!, $input: UpdateStateInput!) {\n    updateState(id: $id, input: $input) { id name color isDefault }\n  }\n"): (typeof documents)["\n  mutation UpdateState($id: ID!, $input: UpdateStateInput!) {\n    updateState(id: $id, input: $input) { id name color isDefault }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation ReorderStates($projectId: ID!, $stateIds: [ID!]!) {\n    reorderStates(projectId: $projectId, stateIds: $stateIds) { id position }\n  }\n"): (typeof documents)["\n  mutation ReorderStates($projectId: ID!, $stateIds: [ID!]!) {\n    reorderStates(projectId: $projectId, stateIds: $stateIds) { id position }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteState($id: ID!, $moveIssuesTo: ID) {\n    deleteState(id: $id, moveIssuesTo: $moveIssuesTo)\n  }\n"): (typeof documents)["\n  mutation DeleteState($id: ID!, $moveIssuesTo: ID) {\n    deleteState(id: $id, moveIssuesTo: $moveIssuesTo)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query StateIssueCount($filter: IssueFilter) {\n    issues(filter: $filter, first: 1) { totalCount }\n  }\n"): (typeof documents)["\n  query StateIssueCount($filter: IssueFilter) {\n    issues(filter: $filter, first: 1) { totalCount }\n  }\n"];

export function graphql(source: string) {
  return (documents as any)[source] ?? {};
}

export type DocumentType<TDocumentNode extends DocumentNode<any, any>> = TDocumentNode extends DocumentNode<  infer TType,  any>  ? TType  : never;