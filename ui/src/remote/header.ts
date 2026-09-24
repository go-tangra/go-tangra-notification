import '@/main.css'
import HeaderBell from '@/components/HeaderBell.vue'

// The shell renders this in its app bar (contracts/shell-changes.md). It shows
// the inbox unread badge and opens the live stream; it renders nothing (and
// opens no connection) for a person without inbox:read, which the shell only
// mounts for modules the person can reach.
export default HeaderBell
