## Your task is simple and singular:

Enforce the following way to do comments, everywhere:

Grouping/sectional comments:

// This comment groups all of the code and comments below it
//
[code directly here, or a comment after which there's code]

Comments about WHY the thing done directly below it is being done:

// This comment explains this line:
[code here]

Also acceptable as multi-line:
// This comment explains WHY the code below it exists
// More explanation
// ... as many // lines as needed
[code here]

---

In CSS, the only acceptable pattern is:

/* --- sectional/grouping comment --- */
["code" directly below]

/* explains WHY we do things the way we do, directly below */
["code" directly below]

/* explains WHY we do things the way
   we do, directly below
*/
["code" directly below]

---

In shell, comments are done as:

# This comment groups all of the code and comments below it
#
[code directly here, or a comment after which there's code]

Comments about WHY the thing done directly below it is being done:

# This comment explains this line:
[code here]

Also acceptable as multi-line:
# This comment explains WHY the code below it exists
# More explanation
# ... as many // lines as needed
[code here]

---

In Lua, comments are done the same as in Shell but using the comment syntax of Lua

---

Ultimately, your task is to replace AI-looking comments and useless ones.
A comment is useless if it does not help a maintainer unfamiliar with the codebase.
A comment is bad if it has non-ASCII characters.
A comment is bad if it cites a spec section by number instead of by name.

In all cases, there AINT a newline before ["code" directly below]/[code or comment after which there's code directly below]
