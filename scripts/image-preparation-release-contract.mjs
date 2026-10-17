// One version only. This records a waived paid matrix, never a passed matrix.
export function validateImagePreparationRelease(receipt, sourceDigest) {
  const fail = message => { throw new Error(`v1.7.26 targeted release: ${message}`); };
  const evidence = value => Array.isArray(value) && value.length > 0 && value.every(x => typeof x === 'string' && x.trim());
  const bound = value => value?.sourceDigest === sourceDigest && evidence(value.evidence);
  if (receipt.version !== 'v1.7.26' || !/^[a-f0-9]{64}$/.test(sourceDigest) || receipt.sourceDigest !== sourceDigest
    || receipt.ownerException?.scope !== 'image-reference-preparation-20261017'
    || receipt.ownerException.approvedBy !== 'Ender'
    || receipt.ownerException.instruction !== '本版豁免付费矩阵，专项回归、独立 review、CI 和四平台安装升级门禁通过后发布；历史未知账务继续保留（推荐）'
    || !bound(receipt.ownerException) || receipt.releaseAuthorization?.scope !== 'v1.7.26 only'
    || receipt.releaseAuthorization.additionalHumanReplyRequired !== false || !bound(receipt.releaseAuthorization)
    || receipt.baseRelease !== 'v1.7.25' || receipt.baseCommit !== '5f3c15c04c25eb4a786cfbe208091360c3eeb5e5'
    || JSON.stringify(receipt.includedPullRequests) !== '[186]') fail('exact authorization, base and repair scope required');
  if (['paidSubmissionLimit', 'newBudgetCNY', 'newSpentCNY', 'newPendingCNY'].some(key => receipt[key] !== 0)
    || !Array.isArray(receipt.cases) || receipt.cases.length || !Array.isArray(receipt.billingAttempts) || receipt.billingAttempts.length
    || ['budgetCNY', 'spentCNY', 'pendingCNY'].some(key => receipt[key] !== null)
    || receipt.liveMatrixStatus !== 'not_run_owner_waived' || receipt.agentAcceptanceStatus !== 'not_run_owner_waived'
    || receipt.acceptanceComplete !== false) fail('paid matrix remains unexecuted; historical totals remain unknown');
  const prior = receipt.priorFinancialUncertainty;
  if (prior?.status !== 'unresolved' || prior.carriedFromVersion !== 'v1.7.25' || prior.pendingCNY !== null
    || !evidence(prior.evidence) || !prior.evidence.includes('docs/release-evidence/v1.7.25.json')) fail('retain historical uncertainty');
  if (receipt.review?.result !== 'approved' || receipt.review.independent !== true || !bound(receipt.review)
    || typeof receipt.review.reviewer !== 'string' || !receipt.review.reviewer.trim()
    || receipt.upgrade?.preservedData !== true || !bound(receipt.upgrade)) fail('current-source review and data preservation required');
  for (const [id, method] of Object.entries({
    userScenario: 'synthetic-reference-and-fault-tests', generationAndRecovery: 'test',
    errorContract: 'test', noNewPaidGeneration: 'source-diff', ci: 'github-actions',
  })) {
    const item = receipt.verification?.[id];
    if (item?.status !== 'passed' || item.method !== method || !bound(item)) fail(`missing check: ${id}`);
  }
  const gate = receipt.verification?.localReleaseGate;
  if (!['passed', 'pending_release_workflow'].includes(gate?.status) || gate.method !== 'local-script'
    || gate.beforeDraft !== true || !bound(gate) || !gate.evidence.includes('.github/workflows/release-desktop.yml')) fail('local release gate must run before draft');
  const packages = receipt.packages;
  const platforms = ['darwin-arm64', 'darwin-amd64', 'windows-amd64', 'linux-amd64'];
  if (!Array.isArray(packages?.platforms) || packages.platforms.length !== 4
    || new Set(packages.platforms).size !== 4 || platforms.some(x => !packages.platforms.includes(x))
    || packages.status !== 'pending_release_workflow' || packages.archives !== null
    || receipt.releasePublished !== false || receipt.releaseComplete !== false
    || ['windowsReleasedUpgradeAndRollbackBeforeUpload', 'finalArchiveSmokeBeforeUpload', 'linuxNativeWindowBeforeUpload', 'signedManifestBeforePublish', 'publicReadbackAfterPublish'].some(key => packages[key] !== true)
    || !evidence(packages.workflowEvidence) || !packages.workflowEvidence.includes('.github/workflows/release-desktop.yml')) fail('all four package and readback gates remain mandatory');
}
