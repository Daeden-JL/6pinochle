import re
import sys

def check_file(filepath):
    print(f"Checking {filepath}...")
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    # Find all <script> blocks
    script_regex = re.compile(r'<script\b[^>]*>(.*?)</script>', re.DOTALL | re.IGNORECASE)
    matches = script_regex.findall(content)
    
    for idx, script in enumerate(matches, 1):
        if not script.strip():
            continue
        
        stack = []
        mapping = {')': '(', '}': '{', ']': '['}
        lines = script.split('\n')
        
        in_string = None
        escaped = False
        in_comment = None # 'line' or 'block'
        in_regex = False
        
        for line_num, line in enumerate(lines, 1):
            char_idx = 0
            while char_idx < len(line):
                c = line[char_idx]
                
                if in_comment == 'line':
                    break
                elif in_comment == 'block':
                    if c == '*' and char_idx + 1 < len(line) and line[char_idx+1] == '/':
                        in_comment = None
                        char_idx += 2
                        continue
                    char_idx += 1
                    continue
                
                if escaped:
                    escaped = False
                    char_idx += 1
                    continue
                
                if c == '\\':
                    escaped = True
                    char_idx += 1
                    continue
                
                if in_string:
                    if c == in_string:
                        in_string = None
                    char_idx += 1
                    continue
                
                if in_regex:
                    if c == '/':
                        in_regex = False
                    char_idx += 1
                    continue
                
                # Check for comment start
                if c == '/' and char_idx + 1 < len(line):
                    if line[char_idx+1] == '/':
                        in_comment = 'line'
                        break
                    elif line[char_idx+1] == '*':
                        in_comment = 'block'
                        char_idx += 2
                        continue
                
                # Check for regex literal start (highly simplified check)
                # In JS, regex start / is preceded by operators, assignment, open parens, or is at the start of a statement.
                # Here we just look if it's a slash and not a comment, and check if it might be a regex.
                # A simple heuristic: if c is '/' and not followed by '/' or '*', and we are not in comment/string/regex.
                # To distinguish division from regex: division is usually preceded by a variable or number.
                # For our script verification, we can assume slashes followed by chars and ended by slash on same line are regexes.
                if c == '/':
                    # Look ahead to see if there's a matching '/' on the same line (not escaped)
                    rem = line[char_idx+1:]
                    # Simple check for closed slash on same line
                    if '/' in rem and not rem.startswith(' '):
                        in_regex = True
                        char_idx += 1
                        continue
                
                # Check string literal start
                if c in ['"', "'", '`']:
                    in_string = c
                    char_idx += 1
                    continue
                
                if c in ['(', '{', '[']:
                    stack.append((c, line_num, char_idx))
                elif c in [')', '}', ']']:
                    if not stack:
                        print(f"Error: Unmatched closing {c} at line {line_num}, col {char_idx} in script {idx}")
                        return False
                    top, top_line, top_col = stack.pop()
                    if mapping[c] != top:
                        print(f"Error: Mismatched closing {c} at line {line_num}, col {char_idx} (matches {top} at line {top_line}, col {top_col}) in script {idx}")
                        return False
                char_idx += 1
            if in_comment == 'line':
                in_comment = None
        
        if stack:
            print(f"Error: Unmatched opening brackets left at end of script {idx}:")
            for top, top_line, top_col in stack[:5]:
                print(f"  {top} at line {top_line}, col {top_col}")
            return False
            
    print("All scripts are perfectly balanced!")
    return True

if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("Usage: python3 check_brackets.py <file>")
        sys.exit(1)
    if not check_file(sys.argv[1]):
        sys.exit(1)
