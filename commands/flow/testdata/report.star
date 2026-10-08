def report(from_="7d", to=None, iteration=None, query=None):
    """What changed since: created, closed, changed and commented, grouped by parent, as markdown.

    The window is [from, to): from_ and to are a date, an RFC 3339 time, or a
    duration back from now (7d, 2w, 12h), and an absent `to` is now. An
    iteration id takes the window from that issue's start and end fields
    instead, which is why an iteration is not a time form: which fields hold
    an iteration's dates is policy, and policy lives here (doc/design/report.md).

    `from_` carries a trailing underscore because `from` is a reserved word in
    Starlark, the same reason `work.schema.import_` does.

    The set is `query`, a jq program over every issue, the archived included,
    run at both ends of the window, so an issue archived during it is reported
    rather than dropped. The default is every issue.

    What is said about each issue is this flow's choice, not the tool's: there
    are no field roles, so `status` and `parent` are named here, in the open.
    Only issues that actually changed are printed. An issue created inside the
    window has no before to diff, so it is described rather than diffed: one
    line of the fields it ends the window with, in the schema's order.
    """

    def short(id):
        return id[:7]

    def name(id):
        # An issue is named by the id it is drawn by, its human_id, which is
        # its Jira key when git-work.display.id asks for one (alias-ids.md A9);
        # an issue neither snapshot has is its short hash.
        for snapshot in (after, before):
            if id in snapshot:
                return snapshot[id]["human_id"]
        return short(id)

    def by_id(items):
        out = {}
        for item in items:
            out[item["id"]] = item
        return out

    def show(value):
        if value == None:
            return "(none)"
        if type(value) == "string":
            return value
        if type(value) == "list":
            parts = []
            for element in value:
                if type(element) == "string":
                    parts.append(element)
                else:
                    parts.append(str(element))
            return "[" + ", ".join(parts) + "]"
        return str(value)

    def is_empty(value):
        # What a described issue leaves out. `False` is empty because the one
        # bool a report meets is `archived`, and a false `archived` is the
        # ordinary case; a zero number is a value and stays.
        return value == None or value == "" or value == [] or value == {} or value == False

    def named_value(value):
        # A relation holds another issue's id, so draw it as that issue; any
        # other full-length id — a person, say — is shortened, because a
        # status report is read, not parsed.
        if type(value) == "string":
            if value in after:
                return "%s %s" % (name(value), show(after[value]["fields"].get("title")))
            if len(value) == 64:
                return short(value)
        return show(value)

    def first_paragraph(text, limit):
        head = text.split("\n\n")[0]
        head = " ".join(head.split())
        if len(head) > limit:
            return head[:limit].rstrip() + "…"
        return head

    # The window. An iteration names an issue whose dates are the window;
    # without them there is nothing to read, so say so and keep from_/to.
    window_from = from_
    window_to = to
    if iteration != None:
        dates = work.issue.get(iteration)["fields"]
        start = dates.get("start")
        end = dates.get("end")
        if start == None:
            work.stderr("report: iteration %s has no start date; using from_=%s" % (short(iteration), from_))
        else:
            window_from = start
            window_to = end
            if end == None:
                work.stderr("report: iteration %s has no end date; the window runs to now" % short(iteration))

    selector = query
    if selector == None:
        selector = "."

    # Every issue at each end of the window, the archived included, for the
    # before/after comparison and for a parent's title; the selection at each
    # end, so that an issue that left the selection during the window is still
    # reported.
    before = by_id(work.issue.list(".", at=window_from, include_archive=True))
    after = by_id(work.issue.list(".", at=window_to, include_archive=True))

    picked = {}
    for item in work.issue.list(selector, at=window_from, include_archive=True):
        picked[item["id"]] = True
    for item in work.issue.list(selector, at=window_to, include_archive=True):
        picked[item["id"]] = True

    # Every operation in the window, grouped by the issue it belongs to, the
    # archived included: an issue archived during the window keeps its
    # operations, the archive among them.
    operations = {}
    for entry in work.issue.log(".", from_=window_from, to=window_to, include_archive=True):
        operations.setdefault(entry["issue"], []).append(entry)

    # Which statuses mean closed, read from the schema's categories and never
    # from a status name (schema.yaml, D2). A status id is read against its
    # own type first, because a field belongs to exactly one type.
    closed_by_type = {}
    closed_anywhere = {}
    # The same read gives the field order a created issue is described in: the
    # export keeps the schema's order, and a report should read the way the
    # issue does rather than alphabetically.
    field_order = {}
    for type_key, spec in work.schema.export()["types"].items():
        fields = spec.get("fields", {})
        field_order[type_key] = fields.keys()
        status = fields.get("status", {})
        closed_by_type[type_key] = {}
        for value in status.get("values", []):
            if value.get("category") in ("completed", "canceled"):
                closed_by_type[type_key][value["id"]] = True
                closed_anywhere[value["id"]] = True

    def described_keys(item):
        # The type's fields in the schema's order, then whatever the issue
        # carries that the schema does not list — an off-schema value Jira
        # holds, a field archived since — so nothing is silently dropped.
        order = field_order.get(item["fields"].get("type"), [])
        keys = []
        for key in order:
            if key in item["fields"]:
                keys.append(key)
        rest = []
        for key in item["fields"]:
            if key not in order:
                rest.append(key)
        return keys + sorted(rest)

    def is_closed(item, status):
        if status == None:
            return False
        type_key = item["fields"].get("type")
        if type_key != None and type_key in closed_by_type:
            return status in closed_by_type[type_key]
        return status in closed_anywhere

    created = 0
    closed = 0
    changed = 0
    commented = 0
    blocks = {}

    for id in picked:
        old = before.get(id)
        new = after.get(id)
        if new == None:
            continue

        marks = []
        lines = []

        if old == None:
            marks.append("created")
            created += 1

        old_fields = {}
        if old != None:
            old_fields = old["fields"]
        new_fields = new["fields"]

        if is_closed(new, new_fields.get("status")) and not is_closed(new, old_fields.get("status")):
            marks.append("closed")
            closed += 1
        if new_fields.get("archived") == True and old_fields.get("archived") != True:
            marks.append("archived")

        path = []
        comments = []
        for entry in operations.get(id, []):
            if entry["type"] == "set-field" and entry["op"]["key"] == "status":
                path.append(show(entry["op"]["value"]))
            elif entry["type"] == "add-comment":
                comments.append(entry)

        if old == None:
            # A created issue has no before, so there is nothing to diff: a
            # column of `(none) → value` is the whole issue written the long
            # way. It is described instead — what it holds at the end of the
            # window, one line, in the schema's order for its type.
            described = []
            for key in described_keys(new):
                if key == "title" or key == "type":
                    # The heading carries the title; the type is the grouping
                    # and the schema order it is read in.
                    continue
                if is_empty(new_fields[key]):
                    continue
                described.append("%s %s" % (key, named_value(new_fields[key])))
            if len(described) > 0:
                lines.append("  with " + ", ".join(described))
        else:
            # Every changed field, because this flow does not know which ones
            # matter and a missing line is worse than an extra one. The status
            # carries the whole path it took, read from the operations: the
            # states a task passed through are often the interesting part of a
            # week.
            keys = {}
            for key in old_fields:
                keys[key] = True
            for key in new_fields:
                keys[key] = True

            field_lines = 0
            for key in sorted(keys):
                was = old_fields.get(key)
                now = new_fields.get(key)
                if was == now:
                    continue
                line = "  %s: %s → %s" % (key, named_value(was), named_value(now))
                if key == "status" and len(path) > 1:
                    line += " (via %s)" % ", ".join(path[:-1])
                lines.append(line)
                field_lines += 1

            if field_lines > 0:
                marks.append("changed")
                changed += 1

        for entry in comments:
            lines.append("  comment — %s, %s: %s" % (
                entry["author"]["name"],
                entry["time"][:10],
                first_paragraph(entry["op"]["message"], 200),
            ))
        if len(comments) > 0:
            commented += 1

        if len(marks) == 0 and len(lines) == 0:
            continue

        suffix = ""
        if len(marks) > 0:
            suffix = " — " + ", ".join(marks)
        block = ["- %s %s%s" % (name(id), show(new_fields.get("title")), suffix)]
        for line in lines:
            block.append(line)

        parent = new_fields.get("parent")
        if type(parent) != "string":
            parent = ""
        blocks.setdefault(parent, []).append(block)

    # Groups in title order, the parentless last, because they are the odds
    # and ends rather than the body of the week.
    named = []
    for parent in blocks:
        if parent == "":
            continue
        title = short(parent)
        if parent in after:
            title = show(after[parent]["fields"].get("title"))
        named.append((title, parent))

    print("# What changed, %s → %s" % (window_from, window_to or "now"))
    print("")
    print("%d created · %d closed · %d changed · %d commented" % (created, closed, changed, commented))

    for pair in sorted(named):
        print("")
        print("## %s" % pair[0])
        print("")
        for block in blocks[pair[1]]:
            for line in block:
                print(line)

    if "" in blocks:
        print("")
        print("## (none)")
        print("")
        for block in blocks[""]:
            for line in block:
                print(line)

    return None
