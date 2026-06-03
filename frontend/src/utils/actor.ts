// Actor IDs follow a "<type>:<name>" convention (e.g. "agent:forsen",
// "user:alice"), mirroring the backend. These helpers parse that scheme so the
// raw prefix strings live in one place.

export const AGENT_PREFIX = 'agent:';
export const USER_PREFIX = 'user:';

export function isAgentId(id: string): boolean {
  return id.startsWith(AGENT_PREFIX);
}

export function isUserId(id: string): boolean {
  return id.startsWith(USER_PREFIX);
}

// agentNameFromId strips the "agent:" prefix, returning "" for non-agent IDs.
export function agentNameFromId(id: string): string {
  return isAgentId(id) ? id.slice(AGENT_PREFIX.length) : '';
}

export function userId(name: string): string {
  return `${USER_PREFIX}${name}`;
}
