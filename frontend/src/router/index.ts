import { createRouter, createWebHistory } from 'vue-router';
import RootView from '@/views/RootView.vue';
import RoomsView from '@/views/RoomsView.vue';
import RoomView from '@/views/RoomView.vue';

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      component: RootView,
      children: [
        { path: '', component: RoomsView },
        { path: 'rooms', component: RoomsView },
        { path: 'rooms/:roomId', component: RoomView },
      ],
    },
  ],
});

export default router;
