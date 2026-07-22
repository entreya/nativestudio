import React from 'react';
import { Button, Typography } from 'antd';
import { CopyOutlined } from '@ant-design/icons';

const { Text } = Typography;
const KEYWORDS = new Set([
  'class', 'const', 'let', 'var', 'function', 'return', 'if', 'else', 'for', 'while', 'switch', 'case',
  'import', 'export', 'from', 'async', 'await', 'new', 'try', 'catch', 'throw', 'public', 'private',
  'protected', 'static', 'interface', 'type', 'struct', 'func', 'package', 'defer', 'go', 'map', 'range',
  'def', 'in', 'and', 'or', 'not', 'None', 'True', 'False', 'echo', 'namespace', 'use', 'extends',
]);

function highlightedTokens(code) {
  const tokenPattern = /(\/\/.*$|#.*$|\/\*[\s\S]*?\*\/|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\b\d+(?:\.\d+)?\b|\b[A-Za-z_$][\w$]*\b)/gm;
  return code.split(tokenPattern).filter(part => part !== '').map((part, index) => {
    let color = '#d8dee9';
    if (/^(\/\/|#|\/\*)/.test(part)) color = '#7f8c75';
    else if (/^["'`]/.test(part)) color = '#a3be8c';
    else if (/^\d/.test(part)) color = '#b48ead';
    else if (KEYWORDS.has(part)) color = '#81a1c1';
    return <span key={index} style={{ color }}>{part}</span>;
  });
}

export default function CodeSnippet({ language = 'text', code }) {
  const copy = async () => {
    try { await navigator.clipboard.writeText(code); } catch (error) { console.error(error); }
  };

  return (
    <div className="chat-code-block">
      <div className="chat-code-header">
        <Text>{language || 'text'}</Text>
        <Button type="text" size="small" icon={<CopyOutlined />} onClick={copy}>Copy</Button>
      </div>
      <pre><code className={`language-${language || 'text'}`}>{highlightedTokens(code)}</code></pre>
    </div>
  );
}
