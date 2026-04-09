/**
 * Fetch full evidence record by ID.
 * @param {string} sessionId
 * @param {string} evidenceId
 * @returns {Promise<Object>}
 */
export async function fetchEvidenceDetail(sessionId, evidenceId) {
  const res = await fetch(`/api/sessions/${sessionId}/evidence/${evidenceId}`);
  if (!res.ok) throw new Error(`Failed to fetch evidence: ${res.status}`);
  return res.json();
}

/**
 * Fetch tool invocation detail by ID.
 * @param {string} sessionId
 * @param {string} invocationId
 * @returns {Promise<Object>}
 */
export async function fetchInvocationDetail(sessionId, invocationId) {
  const res = await fetch(`/api/sessions/${sessionId}/invocations/${invocationId}`);
  if (!res.ok) throw new Error(`Failed to fetch invocation: ${res.status}`);
  return res.json();
}
