def home():
    """Where `git work` opens: this repository's flows, enter running one.

    Bare `git work` runs the flow named home (doc/design/app.md, A8).
    The page lists the flows named here, each with its docstring's first
    line; enter runs one and its view opens over this page, so back is
    back home. The report prints rather than draws, and opens as a page of
    its own. esc r runs any other stored flow, and esc h comes back here.
    """
    work.view.home(flows=["overview", "board", "report"])
