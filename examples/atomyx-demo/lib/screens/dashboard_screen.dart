import 'package:flutter/material.dart';

class DashboardScreen extends StatelessWidget {
  const DashboardScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Dashboard'),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            tooltip: 'Settings',
            onPressed: () => Navigator.pushNamed(context, '/settings'),
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const Text(
            'Welcome',
            style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 8),
          const Text('You are logged in successfully.'),
          const SizedBox(height: 24),

          // Premium Features section
          Card(
            child: ListTile(
              leading: const Icon(Icons.star, color: Colors.amber),
              title: const Text('Premium Features'),
              subtitle: const Text('Access exclusive content'),
              onTap: () {},
            ),
          ),
          const SizedBox(height: 8),

          // Products link
          Card(
            child: ListTile(
              leading: const Icon(Icons.shopping_bag),
              title: const Text('Products'),
              subtitle: const Text('Browse our catalog'),
              onTap: () => Navigator.pushNamed(context, '/products'),
            ),
          ),
          const SizedBox(height: 8),

          // Account section
          Card(
            child: ListTile(
              leading: const Icon(Icons.person),
              title: const Text('Account'),
              subtitle: const Text('Manage your profile'),
              onTap: () {},
            ),
          ),
          const SizedBox(height: 8),

          // Gestures fixture (pointer-command smoke target)
          Card(
            child: ListTile(
              key: const ValueKey('dashboard-gestures-link'),
              leading: const Icon(Icons.touch_app),
              title: const Text('Gestures'),
              subtitle: const Text('Pointer command test fixtures'),
              onTap: () => Navigator.pushNamed(context, '/gestures'),
            ),
          ),
          const SizedBox(height: 24),

          // Upgrade banner (for free users)
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: Colors.blue.shade50,
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Column(
              children: [
                Text(
                  'Upgrade',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                ),
                SizedBox(height: 4),
                Text('Unlock all features with Premium'),
              ],
            ),
          ),
          const SizedBox(height: 24),

          // Sign out
          OutlinedButton(
            onPressed: () {
              showDialog(
                context: context,
                builder: (ctx) => AlertDialog(
                  title: const Text('Sign out'),
                  content: const Text('Are you sure?'),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(ctx),
                      child: const Text('Cancel'),
                    ),
                    FilledButton(
                      onPressed: () {
                        Navigator.pop(ctx);
                        Navigator.pushReplacementNamed(context, '/login');
                      },
                      child: const Text('Confirm'),
                    ),
                  ],
                ),
              );
            },
            child: const Text('Sign out'),
          ),
        ],
      ),
    );
  }
}
