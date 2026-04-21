/// Mock API service — simulates backend responses with configurable delays.
/// Responses match the shapes expected by example YML scripts.
class MockApi {
  static const _delay = Duration(milliseconds: 800);

  /// POST /api/auth/login
  static Future<Map<String, dynamic>> login(String email, String password) async {
    await Future.delayed(_delay);

    if (email.isEmpty || password.isEmpty) {
      return {
        'status': 400,
        'error': 'Email and password required',
      };
    }

    // Simulate OTP requirement for specific emails
    final requiresOtp = email.contains('otp');

    return {
      'status': 200,
      'token': 'tok_${DateTime.now().millisecondsSinceEpoch}',
      'user': {
        'id': 'usr_001',
        'email': email,
        'name': 'Test User',
        'active': true,
      },
      'requires_otp': requiresOtp,
      'session': {
        'ttl': 3600,
      },
    };
  }

  /// POST /api/auth/verify-otp
  static Future<Map<String, dynamic>> verifyOtp(String code) async {
    await Future.delayed(_delay);

    if (code == '123456') {
      return {'status': 200, 'verified': true};
    }
    return {'status': 400, 'error': 'Invalid OTP'};
  }

  /// GET /api/products
  static Future<Map<String, dynamic>> getProducts() async {
    await Future.delayed(_delay);
    return {
      'status': 200,
      'success': true,
      'message': 'Products loaded successfully',
      'data': [
        {'id': 1, 'name': 'Product A', 'price': 29.99, 'category': 'electronics'},
        {'id': 2, 'name': 'Product B', 'price': 49.99, 'category': 'electronics'},
        {'id': 3, 'name': 'Product C', 'price': 9.99, 'category': 'accessories'},
      ],
      'pagination': {
        'page': 1,
        'perPage': 20,
        'total': 3,
      },
    };
  }

  /// GET /api/user/profile
  static Future<Map<String, dynamic>> getProfile() async {
    await Future.delayed(_delay);
    return {
      'status': 200,
      'subscription': 'premium',
      'name': 'Test User',
      'email': 'user@example.com',
    };
  }
}
