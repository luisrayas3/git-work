def overview(group_by="type"):
    """The shape of the work: open stories, open decisions, and open tasks that belong to no story.

    Open reads the status category, never the name (schema.yaml, D2):
    anything whose status is neither completed nor canceled.
    A task under a story is that story's business and stays out of the way;
    one with no parent is listed so it can be given one.
    """
    closed = []
    for name, spec in work.schema.export()["types"].items():
        status = spec.get("fields", {}).get("status", {})
        ids = [
            value["id"]
            for value in status.get("values", [])
            if value.get("category") in ("completed", "canceled")
        ]
        closed.append('"%s":[%s]' % (name, ",".join(['"%s"' % id for id in ids])))

    query = """
        {%s} as $closed
        | map(select(
            (.fields.type == "story"
             or .fields.type == "decision"
             or (.fields.type == "task" and .fields.parent == null))
            and (.fields.status as $status
             | $closed[.fields.type] // [] | index($status) | not)))
    """ % ",".join(closed)

    work.view.list(
        query=query,
        fields=["type", "status", "priority", "title"],
        group_by=group_by,
    )
