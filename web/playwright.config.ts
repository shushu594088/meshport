import {defineConfig} from '@playwright/test'
export default defineConfig({testDir:'./tests',workers:1,retries:0,timeout:60000,use:{baseURL:process.env.MESHPORT_TEST_URL||'http://127.0.0.1:18888',trace:'retain-on-failure'},reporter:[['list'],['html',{open:'never'}]]})
