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
    "\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n": typeof types.PickIssuesDocument,
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
    "\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n": types.PickIssuesDocument,
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
export function graphql(source: "\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n"): (typeof documents)["\n  query PickIssues($filter: IssueFilter) {\n    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {\n      nodes { id identifier title }\n    }\n  }\n"];

export function graphql(source: string) {
  return (documents as any)[source] ?? {};
}

export type DocumentType<TDocumentNode extends DocumentNode<any, any>> = TDocumentNode extends DocumentNode<  infer TType,  any>  ? TType  : never;