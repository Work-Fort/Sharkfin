interface TypingIndicatorProps {
  typingUsers: string[];
}

export function TypingIndicator(props: TypingIndicatorProps) {
  const text = () => {
    const users = props.typingUsers;
    if (users.length === 0) return '';
    if (users.length === 1) return `${users[0]} is typing\u2026`;
    return `${users.join(' and ')} are typing\u2026`;
  };

  return <div class="sf-typing">{text()}</div>;
}
