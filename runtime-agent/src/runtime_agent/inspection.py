"""Build kernel inspection requests and normalize their results."""

import ast
import json

PYTHON_EXPRESSION = """__import__('json').dumps([
    {'name': name, 'type': type(value).__name__,
     'preview': repr(value)[:200] if type(value) in
         (int, float, bool, str, type(None)) else None}
    for name, value in list(get_ipython().user_ns.items())
    if not name.startswith('_') and name not in
        ('In', 'Out', 'get_ipython', 'exit', 'quit', 'open')
][:200], ensure_ascii=False)"""

# IRkernel versions do not consistently implement user_expressions. Capture the
# stdout of a dedicated request, never the stdout of an unrelated user execution.
R_CODE = """local({
    names <- head(ls(envir=.GlobalEnv, all.names=FALSE), 200L)
    values <- lapply(names, function(name) {
        if (bindingIsActive(name, .GlobalEnv))
            return(list(name=name, type="active_binding", preview=NULL))
        value <- get(name, envir=.GlobalEnv, inherits=FALSE)
        simple <- is.atomic(value) && length(value) == 1L && !is.object(value)
        list(name=name, type=typeof(value),
             preview=if (simple) substr(as.character(value), 1L, 200L) else NULL)
    })
    cat(jsonlite::toJSON(values, auto_unbox=TRUE, null="null", na="null"))
})"""


def request_content(command, language):
    """Build the content of a Jupyter execute_request.

    Args:
        command (dict): Validated command including execution_id, kind, and
            timeout_seconds.
        language (str): Validated kernel language, python or r.

    Returns:
        dict: Execution content with stdin disabled and language-specific inspection
        settings.
    """
    inspect = command["kind"] == "inspect"
    return {
        "code": R_CODE if inspect and language == "r" else command.get("code", ""),
        "silent": inspect and language == "python",
        "store_history": not inspect,
        "allow_stdin": False,
        "stop_on_error": True,
        "user_expressions": {"variables": PYTHON_EXPRESSION}
        if inspect and language == "python"
        else {},
    }


def result(reply, language, stdout):
    """Decode and validate variable inspection output.

    Args:
        reply (dict): Successful execute_reply content, including Python user
            expressions.
        language (str): Validated kernel language, python or r.
        stdout (str): Captured stdout of the R inspection request; unused for Python.

    Returns:
        dict: A variables list containing up to 200 name, type, and preview descriptors.

    Raises:
        ValueError: Inspection failed or returned invalid JSON or variable descriptors.
        KeyError: A required Python expression result field is missing.
        SyntaxError: The Python text/plain result is not a valid literal.
    """
    if language == "python":
        expression = reply.get("user_expressions", {}).get("variables", {})
        if expression.get("status") != "ok":
            raise ValueError("Variable inspection failed")
        values = json.loads(ast.literal_eval(expression["data"]["text/plain"]))
    else:
        values = json.loads(stdout)
    if not isinstance(values, list) or len(values) > 200:
        raise ValueError("Invalid variable inspection result")
    for value in values:
        if (
            not isinstance(value, dict)
            or not isinstance(value.get("name"), str)
            or not isinstance(value.get("type"), str)
        ):
            raise ValueError("Invalid variable descriptor")
        preview = value.get("preview")
        if preview is not None and (not isinstance(preview, str) or len(preview) > 200):
            raise ValueError("Invalid variable preview")
    return {"variables": values}
