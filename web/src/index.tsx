export default function SharkfinApp(props: { connected: boolean }) {
  return <div>Sharkfin Chat (connected: {String(props.connected)})</div>;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};
