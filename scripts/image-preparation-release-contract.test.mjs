import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateImagePreparationRelease } from './image-preparation-release-contract.mjs';

const digest = 'a'.repeat(64);
function fixture() {
  const bound = { sourceDigest: digest, evidence: ['synthetic-test-fixture'] };
  return {
    ...bound, version: 'v1.7.26', baseRelease: 'v1.7.25', baseCommit: '5f3c15c04c25eb4a786cfbe208091360c3eeb5e5', includedPullRequests: [186],
    ownerException: { ...bound, scope: 'image-reference-preparation-20261017', approvedBy: 'Ender', instruction: '本版豁免付费矩阵，专项回归、独立 review、CI 和四平台安装升级门禁通过后发布；历史未知账务继续保留（推荐）' },
    releaseAuthorization: { ...bound, scope: 'v1.7.26 only', additionalHumanReplyRequired: false },
    paidSubmissionLimit: 0, newBudgetCNY: 0, newSpentCNY: 0, newPendingCNY: 0, cases: [], billingAttempts: [],
    budgetCNY: null, spentCNY: null, pendingCNY: null, liveMatrixStatus: 'not_run_owner_waived', agentAcceptanceStatus: 'not_run_owner_waived', acceptanceComplete: false,
    priorFinancialUncertainty: { status: 'unresolved', carriedFromVersion: 'v1.7.25', pendingCNY: null, evidence: ['docs/release-evidence/v1.7.25.json'] },
    review: { ...bound, result: 'approved', independent: true, reviewer: 'test reviewer' }, upgrade: { ...bound, preservedData: true },
    verification: Object.fromEntries(Object.entries({ userScenario: 'synthetic-reference-and-fault-tests', generationAndRecovery: 'test', errorContract: 'test', noNewPaidGeneration: 'source-diff', ci: 'github-actions' }).map(([id, method]) => [id, { ...bound, method, status: 'passed' }])),
    packages: { platforms: ['darwin-arm64', 'darwin-amd64', 'windows-amd64', 'linux-amd64'], status: 'pending_release_workflow', archives: null, windowsReleasedUpgradeAndRollbackBeforeUpload: true, finalArchiveSmokeBeforeUpload: true, linuxNativeWindowBeforeUpload: true, signedManifestBeforePublish: true, publicReadbackAfterPublish: true, workflowEvidence: ['.github/workflows/release-desktop.yml'] },
    releasePublished: false, releaseComplete: false,
  };
}
function valid() {
  const r = fixture();
  r.verification.localReleaseGate = { sourceDigest: digest, evidence: ['.github/workflows/release-desktop.yml'], status: 'pending_release_workflow', method: 'local-script', beforeDraft: true };
  return r;
}
test('one-version waiver preserves unpaid status and pending final packages', () => {
  assert.doesNotThrow(() => validateImagePreparationRelease(valid(), digest));
});
for (const [name, mutate] of Object.entries({
  'other version': r => r.version = 'v1.7.27',
  'missing owner': r => r.ownerException.approvedBy = null,
  'old review': r => r.review.sourceDigest = 'b'.repeat(64),
  'paid call': r => r.newSpentCNY = 0.4,
  'zeroed historical uncertainty': r => r.pendingCNY = 0,
  'invented matrix pass': r => r.liveMatrixStatus = 'passed',
  'unrelated PR': r => r.includedPullRequests.push(185),
  'missing scenario': r => delete r.verification.userScenario,
  'missing final platform': r => r.packages.platforms.pop(),
  'disabled upgrade check': r => r.packages.windowsReleasedUpgradeAndRollbackBeforeUpload = false,
  'premature publication': r => r.releasePublished = true,
  'unbound local gate': r => r.verification.localReleaseGate.sourceDigest = 'b'.repeat(64),
})) test(`reject ${name}`, () => {
  const receipt = valid(); mutate(receipt);
  assert.throws(() => validateImagePreparationRelease(receipt, digest));
});
