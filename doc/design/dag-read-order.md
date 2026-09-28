# entity/dag cannot read back a merge of two unequal branches

Status: **fixed here on 2026-09-28 by option (b), an owner decision (`7cb8b39`);
unfixed upstream.** `dag.read` now sorts the commits parents first
(Kahn's algorithm, `parentsFirst`) and iterates them forwards,
the cleaner rewrite noted under (b), in place of the post-order DFS diff below.
`entity/dag`'s `TestMergeUnevenBranches` and the repro below, no longer skipped, cover it.
Hit on the tracker by `8ade811` in a real pull, where the symptom was the `panic: DFS failed`.

## Reproduction

`cache/dag_divergence_test.go` (committed with this document):
two clones sharing an identity through a bare remote, one entity pushed from A to B,
A commits `local` times, B commits `remote` times and pushes, A runs `RepoCache.Pull`.
Each commit is one ordinary cache write (`PlanSetFields` + `CommitOperations` on
`entities/issue`; `AddComment` + `Commit` on git-bug's own `entities/bug`).
No jira code, no NoOp markers, no `IssueCache.Update`.

| local (A, puller) | remote (B) | result |
| --- | --- | --- |
| 1 | 1 | ok |
| 2 | 2 | ok |
| 2 | 1 | ok |
| 3 | 1 | `creation lamport time not set` |
| 5 | 2 | `creation lamport time not set` |
| 1 | 2 | `creation lamport time not set` |
| 1 | 3 | `creation lamport time not set` |
| 2 | 5 | `creation lamport time not set` |

Identical on the issue entity and the legacy bug entity, so it is not ours
(`cache/cached.go` reloadLocked/rebaseStaged, 8254ee05, is not involved:
the failure is inside `dag.read`). The failing cases were skipped until the fix;
they now run with the rest of the suite.

The reporter's diagnosis is right, but the condition is broader than "remote longer":
it reads back only when `local == remote` or `local == remote + 1`.

### Damage

`dag.merge` writes the merge commit and **updates the local ref before anything
reads it back**; the read fails afterwards (in the cache's MergeAll). Verified:
after the failed pull, `refs/work-issues/<id>` points at the 2-parent merge commit
and `issue.Read` fails on it. So the issue becomes unreadable locally, and a
subsequent `git work push` would publish that merge commit, making it unreadable
on every clone (the parent order is baked into the commit).
The stored DAG itself is valid: fixing the reader makes these commits readable
again, no data is lost and no format change is needed.

## Root cause

`entity/dag/entity.go`, `read()` (lines ~93-187): it BFS-walks parents from the
head and then iterates `BFSOrder` in reverse, assuming this is a topological order
(parents before children). It is not. The merge commit `M` has parents
`(local, remote)` (`entity_actions.go`, `opp.Write(def, repo, localCommit, remoteCommit)`).
Example local=1, remote=2, root R:

    BFS from M:  M, A1, B2, R, B1      (R is discovered via A1 at depth 2, B1 at depth 2 after it)
    reversed:    B1, R, B2, A1, M

The loop treats the last BFS element (B1) as the root: `isFirstCommit && CreateTime <= 0`
fires -> "creation lamport time not set". Had that check passed, the next check
(`oppMap[parentHash]` missing) would `panic("DFS failed")`. In general, reversed BFS
is topological only if every commit's depth from the head is greater than each of its
children's along *every* path, which fails as soon as two paths to a commit differ in
length by more than the queue order happens to hide.

`readClockNoCheck` is unaffected (it walks first parents to the root only).

## Upstream status

- Upstream master (`github.com/git-bug/git-bug`, checked 2026-09-28) still has the
  identical reversed-BFS loop in `read()`. Commits touching `entity/dag` since the
  fork (e1c21a42): 442e9ea4 (enforce ID matching the first commit), adf0231c,
  e3c6cade, ea574418, 72e4b029, 04c9d245 — none changes the ordering.
- No open issue names it. Closed issue #845 "`git bug pull` fails with panic: DFS
  failed" (2022) is very likely the same defect (the panic is the other symptom of
  the same ordering bug), dismissed at the time as data from a pre-stable model.
- So there is nothing to cherry-pick.

## Options

(a) Cherry-pick upstream: **not available**. Worth filing upstream with this repro.

(b) Minimal patch to `entity/dag/entity.go` (owner decision, flagged; not applied).
Replace the order with a real topological one — an iterative post-order DFS over
parents — and leave the validation loop untouched. The root is necessarily first
(post-order emits a parentless commit first), so the single-root, clock-causality
and merge-has-no-ops checks keep their meaning; nothing in the format changes.
Verified in a scratch copy (`/tmp/gw-dagfix`, not the worktree): with the patch
`go test ./entity/... ./entities/... ./cache/...` passes, including all 10 unskipped
repro cases.

```diff
@@ -121,7 +121,44 @@ func read[EntityT entity.Interface](...)
 		}
 	}
 
-	// Now, we can reverse this topological order and read the commits in an order where
+	// A reversed BFS is not a topological order: when two branches of a merge
+	// differ in length, the root is reached through the short one before the
+	// long one is exhausted. Order the commits so that each one comes after all
+	// of its parents instead (iterative post-order DFS over the parents).
+	commits := make(map[repository.Hash]repository.Commit, len(BFSOrder))
+	for _, commit := range BFSOrder {
+		commits[commit.Hash] = commit
+	}
+	topoOrder := make([]repository.Commit, 0, len(BFSOrder))
+	emitted := make(map[repository.Hash]struct{}, len(BFSOrder))
+	type frame struct {
+		commit repository.Commit
+		next   int
+	}
+	stack := []frame{{commit: BFSOrder[0]}}
+	for len(stack) > 0 {
+		top := &stack[len(stack)-1]
+		if top.next < len(top.commit.Parents) {
+			parent := top.commit.Parents[top.next]
+			top.next++
+			if _, ok := emitted[parent]; !ok {
+				stack = append(stack, frame{commit: commits[parent]})
+			}
+			continue
+		}
+		if _, ok := emitted[top.commit.Hash]; !ok {
+			emitted[top.commit.Hash] = struct{}{}
+			topoOrder = append(topoOrder, top.commit)
+		}
+		stack = stack[:len(stack)-1]
+	}
+	// Reversed, so that the loop below reads it from the end as before.
+	for i, j := 0, len(topoOrder)-1; i < j; i, j = i+1, j-1 {
+		topoOrder[i], topoOrder[j] = topoOrder[j], topoOrder[i]
+	}
+	BFSOrder = topoOrder
+
+	// Now, we can read the commits in reverse, an order where
 	// we are sure to have read all the chronological ancestors when we read a commit.
```

(A cleaner rewrite would iterate `topoOrder` forwards with `isFirstCommit := i == 0`;
the version above keeps the diff to one inserted block.)

(c) Workaround above the boundary: none that is sound. `read` is the only reader,
and the merge commit's shape is fixed by `dag.merge`. Padding the shorter branch
with no-op commits before merging only works for a single fork point, not for
histories that already contain merges, and a cache-side rebase instead of merge
would reimplement `dag.merge` outside dag (the AGENTS.md "second implementation"
hazard). Tests can keep diverging by equal amounts (as TestTwoClones does), but
real use cannot.

**Recommendation: (b)**, as a recorded owner decision (a second pristine edit next
to the identity ref-name change), plus an upstream issue/PR carrying this test, so
the patch can be dropped when upstream takes it. Until then any pull after unequal
divergence corrupts the local ref of that entity, on every dag namespace
(`work-issues`, `work-schema`, `work-flows`, and git-bug's `refs/issues`; identities are not dag entities).
Recovery for an entity already hit: `git update-ref refs/work-issues/<id> <merge>^1`
(its pre-merge local head) — or apply the patch, which reads the merge as is.
