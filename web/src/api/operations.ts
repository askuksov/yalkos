import { graphql } from '../gql'

export const SetupStatusQuery = graphql(`
  query SetupStatus {
    setupStatus { needed }
  }
`)

export const ViewerQuery = graphql(`
  query Viewer {
    viewer { id name email role }
  }
`)

export const SetupMutation = graphql(`
  mutation Setup($input: SetupInput!) {
    setup(input: $input) { id }
  }
`)

export const LoginMutation = graphql(`
  mutation Login($email: String!, $password: String!) {
    login(email: $email, password: $password) { id }
  }
`)

export const LogoutMutation = graphql(`
  mutation Logout {
    logout
  }
`)

export const AcceptInviteMutation = graphql(`
  mutation AcceptInvite($input: AcceptInviteInput!) {
    acceptInvite(input: $input) { id }
  }
`)

export const MembersQuery = graphql(`
  query Members {
    members { id name email role disabled }
  }
`)

export const InvitesQuery = graphql(`
  query Invites {
    invites { id role expiresAt }
  }
`)

export const CreateInviteMutation = graphql(`
  mutation CreateInvite($role: Role!, $days: Int) {
    createInvite(role: $role, expiresInDays: $days) { url invite { id } }
  }
`)

export const RevokeInviteMutation = graphql(`
  mutation RevokeInvite($id: ID!) {
    revokeInvite(id: $id)
  }
`)

export const UpdateMemberRoleMutation = graphql(`
  mutation UpdateMemberRole($userId: ID!, $role: Role!) {
    updateMemberRole(userId: $userId, role: $role) { id role }
  }
`)

export const SetMemberDisabledMutation = graphql(`
  mutation SetMemberDisabled($userId: ID!, $disabled: Boolean!) {
    setMemberDisabled(userId: $userId, disabled: $disabled) { id disabled }
  }
`)

export const ApiKeysQuery = graphql(`
  query ApiKeys {
    apiKeys { id name displayPrefix createdAt lastUsedAt }
  }
`)

export const CreateApiKeyMutation = graphql(`
  mutation CreateApiKey($name: String!) {
    createApiKey(name: $name) { key apiKey { id } }
  }
`)

export const RevokeApiKeyMutation = graphql(`
  mutation RevokeApiKey($id: ID!) {
    revokeApiKey(id: $id)
  }
`)

export const ProjectsQuery = graphql(`
  query Projects($includeArchived: Boolean) {
    projects(includeArchived: $includeArchived, first: 250) {
      nodes { id key name icon color archivedAt isMember }
    }
  }
`)

export const ProjectQuery = graphql(`
  query Project($id: String!) {
    project(id: $id) {
      id key name description icon color archivedAt isMember
      states { id name type color position isDefault }
      members { id name }
    }
  }
`)

export const WebhookDeliveryFields = graphql(`
  fragment WebhookDeliveryFields on WebhookDelivery {
    id resource action entityId status attempts responseStatus error durationMs
    createdAt lastAttemptAt nextAttemptAt
  }
`)

export const WebhookFields = graphql(`
  fragment WebhookFields on Webhook {
    id name url secret enabled disabledReason disabledAt resources
    project { id key name }
    createdAt updatedAt
  }
`)

// The list shows only the latest delivery per webhook: a long delivery list per webhook would exceed the query cost budget.
export const WebhooksQuery = graphql(`
  query Webhooks {
    webhooks {
      id name url enabled disabledReason resources
      project { id key }
      deliveries(first: 1) { ...WebhookDeliveryFields }
    }
  }
`)

export const WebhookQuery = graphql(`
  query Webhook($id: ID!) {
    webhook(id: $id) {
      ...WebhookFields
      deliveries(first: 50) { ...WebhookDeliveryFields }
    }
  }
`)

export const CreateWebhookMutation = graphql(`
  mutation CreateWebhook($input: CreateWebhookInput!) {
    createWebhook(input: $input) { id }
  }
`)

export const UpdateWebhookMutation = graphql(`
  mutation UpdateWebhook($id: ID!, $input: UpdateWebhookInput!) {
    updateWebhook(id: $id, input: $input) { ...WebhookFields }
  }
`)

export const DeleteWebhookMutation = graphql(`
  mutation DeleteWebhook($id: ID!) {
    deleteWebhook(id: $id)
  }
`)

export const RotateWebhookSecretMutation = graphql(`
  mutation RotateWebhookSecret($id: ID!) {
    rotateWebhookSecret(id: $id) { id secret }
  }
`)

export const TestWebhookMutation = graphql(`
  mutation TestWebhook($id: ID!) {
    testWebhook(id: $id) { ...WebhookDeliveryFields }
  }
`)
